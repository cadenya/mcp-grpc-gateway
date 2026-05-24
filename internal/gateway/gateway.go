package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"cadenya.com/mcp-grpc-gateway/internal/annotations"
	"cadenya.com/mcp-grpc-gateway/internal/grpcinvoke"
	"cadenya.com/mcp-grpc-gateway/internal/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func NewServer(service protoreflect.ServiceDescriptor) *mcp.Server {
	meta := annotations.ForService(service)
	return mcp.NewServer(&mcp.Implementation{
		Name:       meta.Name,
		Title:      meta.Title,
		Version:    meta.Version,
		WebsiteURL: meta.WebsiteURL,
	}, &mcp.ServerOptions{
		Instructions: meta.Instructions,
	})
}

type RegisterOption func(*registerConfig)

type registerConfig struct {
	requireToolAnnotations bool
}

func WithRequireToolAnnotations(require bool) RegisterOption {
	return func(cfg *registerConfig) {
		cfg.requireToolAnnotations = require
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
		inputSchema, err := schema.ForMessage(method.Input())
		if err != nil {
			return fmt.Errorf("build schema for %s: %w", method.FullName(), err)
		}
		registerTool(server, conn, method, meta, inputSchema)
	}
	return nil
}

func registerTool(server *mcp.Server, conn grpc.ClientConnInterface, method protoreflect.MethodDescriptor, meta annotations.ToolMetadata, inputSchema map[string]any) {
	server.AddTool(&mcp.Tool{
		Name:        meta.Name,
		Description: meta.Description,
		InputSchema: inputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
