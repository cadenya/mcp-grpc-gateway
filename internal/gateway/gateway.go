package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.cadenya.com/mcp-grpc-gateway/internal/annotations"
	"go.cadenya.com/mcp-grpc-gateway/internal/grpcinvoke"
	"go.cadenya.com/mcp-grpc-gateway/internal/schema"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type ServerMetadata struct {
	Name         string
	Title        string
	Version      string
	Instructions string
	WebsiteURL   string
}

func NewServer(meta ServerMetadata) *mcp.Server {
	if meta.Name == "" {
		meta.Name = "mcp-grpc-gateway"
	}
	if meta.Version == "" {
		meta.Version = "dev"
	}
	return mcp.NewServer(&mcp.Implementation{
		Name:       meta.Name,
		Title:      meta.Title,
		Version:    meta.Version,
		WebsiteURL: meta.WebsiteURL,
	}, &mcp.ServerOptions{
		Instructions: meta.Instructions,
		GetSessionID: func() string { return "" },
	})
}

type RegisterOption func(*registerConfig)

type registerConfig struct {
	requireToolAnnotations bool
	registeredToolNames    map[string]string
	logger                 *slog.Logger
	toolCallTimeout        time.Duration
	serviceToolPrefix      string
}

func WithRequireToolAnnotations(require bool) RegisterOption {
	return func(cfg *registerConfig) {
		cfg.requireToolAnnotations = require
	}
}

func WithRegisteredToolNames(names map[string]string) RegisterOption {
	return func(cfg *registerConfig) {
		cfg.registeredToolNames = names
	}
}

func WithLogger(logger *slog.Logger) RegisterOption {
	return func(cfg *registerConfig) {
		cfg.logger = logger
	}
}

func WithTimeout(timeout time.Duration) RegisterOption {
	return func(cfg *registerConfig) {
		cfg.toolCallTimeout = timeout
	}
}

func WithToolCallTimeout(timeout time.Duration) RegisterOption {
	return WithTimeout(timeout)
}

func WithServiceToolPrefix(prefix string) RegisterOption {
	return func(cfg *registerConfig) {
		cfg.serviceToolPrefix = prefix
	}
}

func RegisterTools(server *mcp.Server, conn grpc.ClientConnInterface, service protoreflect.ServiceDescriptor, opts ...RegisterOption) error {
	if server == nil {
		return fmt.Errorf("mcp server is nil")
	}
	if conn == nil {
		return fmt.Errorf("grpc connection is nil")
	}
	if service == nil {
		return fmt.Errorf("service descriptor is nil")
	}
	cfg := registerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.registeredToolNames == nil {
		cfg.registeredToolNames = map[string]string{}
	}
	if cfg.logger == nil {
		cfg.logger = slog.Default()
	}
	if cfg.serviceToolPrefix == "" {
		cfg.serviceToolPrefix = annotations.ForService(service).ToolPrefix
	}

	methods := service.Methods()
	for i := 0; i < methods.Len(); i++ {
		method := methods.Get(i)
		if method.IsStreamingClient() || method.IsStreamingServer() {
			continue
		}
		meta := annotations.ForMethod(method)
		if cfg.requireToolAnnotations && !meta.Annotated {
			continue
		}
		meta.Name = cfg.serviceToolPrefix + meta.Name
		if existingService, ok := cfg.registeredToolNames[meta.Name]; ok {
			cfg.logger.Warn("tool name collision",
				"grpc_service", string(service.FullName()),
				"existing_grpc_service", existingService,
				"tool_name", meta.Name,
			)
			continue
		}
		inputSchema, err := schema.ForMessage(method.Input())
		if err != nil {
			return fmt.Errorf("build schema for %s: %w", method.FullName(), err)
		}
		registerTool(server, conn, method, meta, inputSchema, cfg.toolCallTimeout)
		cfg.registeredToolNames[meta.Name] = string(service.FullName())
	}
	return nil
}

func registerTool(server *mcp.Server, conn grpc.ClientConnInterface, method protoreflect.MethodDescriptor, meta annotations.ToolMetadata, inputSchema map[string]any, toolCallTimeout time.Duration) {
	server.AddTool(&mcp.Tool{
		Name:        meta.Name,
		Description: meta.Description,
		InputSchema: inputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if toolCallTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, toolCallTimeout)
			defer cancel()
		}
		out, err := grpcinvoke.InvokeUnary(ctx, conn, method, req.Params.Arguments)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
				IsError: true,
			}, nil
		}
		text, err := json.Marshal(out)
		if err != nil {
			return nil, fmt.Errorf("marshal tool response: %w", err)
		}
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: string(text)}},
			StructuredContent: out,
		}, nil
	})
}
