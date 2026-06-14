package toolregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"text/template"
	"time"

	"go.cadenya.com/mcp-grpc-gateway/internal/annotations"
	"go.cadenya.com/mcp-grpc-gateway/internal/grpcinvoke"
	"go.cadenya.com/mcp-grpc-gateway/internal/schema"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type BuildOptions struct {
	Conn                    grpc.ClientConnInterface
	Services                []protoreflect.ServiceDescriptor
	Server                  ServerMetadata
	Logger                  *slog.Logger
	RequireToolAnnotations  bool
	RegisteredToolNameOwner map[string]string
	ToolCallTimeout         time.Duration
}

type ServerMetadata struct {
	Name         string
	Title        string
	Version      string
	Instructions string
	WebsiteURL   string
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Annotations any            `json:"annotations,omitempty"`

	method          protoreflect.MethodDescriptor
	conn            grpc.ClientConnInterface
	contentTemplate *template.Template
	timeout         time.Duration
}

type Registry struct {
	server ServerMetadata
	tools  []Tool
	byName map[string]Tool
}

func Build(opts BuildOptions) (*Registry, error) {
	if opts.Conn == nil {
		return nil, fmt.Errorf("grpc connection is nil")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	owners := opts.RegisteredToolNameOwner
	if owners == nil {
		owners = map[string]string{}
	}

	registry := &Registry{server: defaultServerMetadata(opts.Server), byName: map[string]Tool{}}
	for _, service := range opts.Services {
		if service == nil {
			return nil, fmt.Errorf("service descriptor is nil")
		}
		prefix := annotations.ForService(service).ToolPrefix
		methods := service.Methods()
		for i := 0; i < methods.Len(); i++ {
			method := methods.Get(i)
			if method.IsStreamingClient() || method.IsStreamingServer() {
				continue
			}
			meta := annotations.ForMethod(method)
			if opts.RequireToolAnnotations && !meta.Annotated {
				continue
			}
			meta.Name = prefix + meta.Name
			if existingService, ok := owners[meta.Name]; ok {
				logger.Warn("tool name collision",
					"grpc_service", string(service.FullName()),
					"existing_grpc_service", existingService,
					"tool_name", meta.Name,
				)
				continue
			}
			inputSchema, err := schema.ForMessage(method.Input())
			if err != nil {
				return nil, fmt.Errorf("build schema for %s: %w", method.FullName(), err)
			}
			var contentTmpl *template.Template
			if meta.ContentTemplate != "" {
				parsed, err := template.New(meta.Name).Parse(meta.ContentTemplate)
				if err != nil {
					logger.Warn("invalid tool content template",
						"grpc_service", string(service.FullName()),
						"tool_name", meta.Name,
						"error", err,
					)
					continue
				}
				contentTmpl = parsed
			}
			tool := Tool{
				Name:            meta.Name,
				Description:     meta.Description,
				InputSchema:     inputSchema,
				method:          method,
				conn:            opts.Conn,
				contentTemplate: contentTmpl,
				timeout:         opts.ToolCallTimeout,
			}
			if meta.Annotations != nil {
				tool.Annotations = meta.Annotations
			}
			registry.tools = append(registry.tools, tool)
			registry.byName[tool.Name] = tool
			owners[tool.Name] = string(service.FullName())
		}
	}
	sort.Slice(registry.tools, func(i, j int) bool {
		return registry.tools[i].Name < registry.tools[j].Name
	})
	return registry, nil
}

func (r *Registry) Server() ServerMetadata {
	if r == nil {
		return defaultServerMetadata(ServerMetadata{})
	}
	return r.server
}

func (r *Registry) Tools() []Tool {
	if r == nil {
		return nil
	}
	out := make([]Tool, len(r.tools))
	copy(out, r.tools)
	return out
}

func defaultServerMetadata(meta ServerMetadata) ServerMetadata {
	if meta.Name == "" {
		meta.Name = "mcp-grpc-gateway"
	}
	if meta.Version == "" {
		meta.Version = "dev"
	}
	return meta
}

func (r *Registry) Tool(name string) (Tool, bool) {
	if r == nil {
		return Tool{}, false
	}
	tool, ok := r.byName[name]
	return tool, ok
}

func (r *Registry) Call(ctx context.Context, name string, arguments json.RawMessage) (map[string]any, error) {
	tool, ok := r.Tool(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	return tool.Call(ctx, arguments), nil
}

func (t Tool) Call(ctx context.Context, arguments json.RawMessage) map[string]any {
	if t.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.timeout)
		defer cancel()
	}
	out, err := grpcinvoke.InvokeUnary(ctx, t.conn, t.method, arguments)
	if err != nil {
		return toolError(err.Error())
	}
	if t.contentTemplate != nil {
		var buf bytes.Buffer
		if err := t.contentTemplate.Execute(&buf, out); err != nil {
			return toolError(err.Error())
		}
		return map[string]any{
			"content": []any{textContent(buf.String())},
		}
	}
	text, err := json.Marshal(out)
	if err != nil {
		return toolError(fmt.Sprintf("marshal tool response: %v", err))
	}
	return map[string]any{
		"content":           []any{textContent(string(text))},
		"structuredContent": out,
	}
}

func toolError(message string) map[string]any {
	return map[string]any{
		"content": []any{textContent(message)},
		"isError": true,
	}
}

func textContent(text string) map[string]any {
	return map[string]any{
		"type": "text",
		"text": text,
	}
}
