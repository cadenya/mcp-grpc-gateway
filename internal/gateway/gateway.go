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

func RegisterTools(server *mcp.Server, conn grpc.ClientConnInterface, service protoreflect.ServiceDescriptor) error {
	if server == nil {
		return fmt.Errorf("mcp server is nil")
	}
	if conn == nil {
		return fmt.Errorf("grpc connection is nil")
	}
	if service == nil {
		return fmt.Errorf("service descriptor is nil")
	}

	methods := service.Methods()
	for i := 0; i < methods.Len(); i++ {
		method := methods.Get(i)
		if method.IsStreamingClient() || method.IsStreamingServer() {
			continue
		}
		inputSchema, err := schema.ForMessage(method.Input())
		if err != nil {
			return fmt.Errorf("build schema for %s: %w", method.FullName(), err)
		}
		meta := annotations.ForMethod(method)
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
