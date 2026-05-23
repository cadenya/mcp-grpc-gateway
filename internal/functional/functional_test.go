package functional_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	grpcmcpgatewayv1 "cadenya.com/mcp-grpc-gateway/gen/grpcmcpgateway/v1"
	"cadenya.com/mcp-grpc-gateway/internal/discovery"
	"cadenya.com/mcp-grpc-gateway/internal/gateway"
	"cadenya.com/mcp-grpc-gateway/internal/testpb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	reflectionv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestAnnotatedGRPCServiceIsExposedThroughMCPEndpoint(t *testing.T) {
	ctx := context.Background()
	grpcAddr, stopGRPC := startGreeterGRPCServer(t)
	defer stopGRPC()

	grpcClient, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer grpcClient.Close()

	service, err := discovery.LoadService(ctx, grpcClient, "functional.v1.GreeterService")
	require.NoError(t, err)

	mcpServer := gateway.NewServer(service)
	require.NoError(t, gateway.RegisterTools(mcpServer, grpcClient, service))

	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{JSONResponse: true}))
	defer httpServer.Close()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "functional-client", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL,
		HTTPClient:           httpServer.Client(),
		DisableStandaloneSSE: true,
	}, nil)
	require.NoError(t, err)
	defer session.Close()

	init := session.InitializeResult()
	require.Equal(t, "greeter", init.ServerInfo.Name)
	require.Equal(t, "Greeter Service", init.ServerInfo.Title)
	require.Equal(t, "1.0.0", init.ServerInfo.Version)
	require.Equal(t, "https://example.com/greeter", init.ServerInfo.WebsiteURL)
	require.Equal(t, "Use this server to greet users by name.", init.Instructions)

	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		require.NoError(t, err)
		tools = append(tools, tool)
	}
	require.Len(t, tools, 1)
	require.Equal(t, "greet_user", tools[0].Name)
	require.Equal(t, "Greets a user by name", tools[0].Description)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "greet_user",
		Arguments: map[string]any{"name": "Ada"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, map[string]any{"greeting": "Hello, Ada"}, result.StructuredContent)
}

func TestLiveGRPCReflectionReturnsToolAnnotations(t *testing.T) {
	ctx := context.Background()
	grpcAddr, stopGRPC := startGreeterGRPCServer(t)
	defer stopGRPC()

	grpcClient, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer grpcClient.Close()

	reflectionClient := reflectionv1alpha.NewServerReflectionClient(grpcClient)
	stream, err := reflectionClient.ServerReflectionInfo(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(&reflectionv1alpha.ServerReflectionRequest{
		MessageRequest: &reflectionv1alpha.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: "functional.v1.GreeterService",
		},
	}))

	resp, err := stream.Recv()
	require.NoError(t, err)
	require.Nil(t, resp.GetErrorResponse())

	methodOptions := reflectedMethodOptions(t, resp.GetFileDescriptorResponse().GetFileDescriptorProto(), "functional/v1/greeter.proto", "GreeterService", "Greet")
	require.True(t, proto.HasExtension(methodOptions, grpcmcpgatewayv1.E_Tool))
	tool := proto.GetExtension(methodOptions, grpcmcpgatewayv1.E_Tool).(*grpcmcpgatewayv1.Tool)
	require.Equal(t, "greet_user", tool.GetName())
	require.Equal(t, "Greets a user by name", tool.GetDescription())

	serviceOptions := reflectedServiceOptions(t, resp.GetFileDescriptorResponse().GetFileDescriptorProto(), "functional/v1/greeter.proto", "GreeterService")
	require.True(t, proto.HasExtension(serviceOptions, grpcmcpgatewayv1.E_Server))
	server := proto.GetExtension(serviceOptions, grpcmcpgatewayv1.E_Server).(*grpcmcpgatewayv1.Server)
	require.Equal(t, "greeter", server.GetName())
	require.Equal(t, "Greeter Service", server.GetTitle())
}

func startGreeterGRPCServer(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	testpb.RegisterGreeterServiceServer(server, &greeterServer{})
	reflection.Register(server)

	go func() {
		_ = server.Serve(listener)
	}()

	return listener.Addr().String(), func() {
		server.Stop()
		_ = listener.Close()
	}
}

type greeterServer struct {
	testpb.UnimplementedGreeterServiceServer
}

func (greeterServer) Greet(_ context.Context, req *testpb.GreetRequest) (*testpb.GreetResponse, error) {
	return &testpb.GreetResponse{Greeting: "Hello, " + req.GetName()}, nil
}

func reflectedMethodOptions(t *testing.T, rawFiles [][]byte, filePath string, serviceName string, methodName string) *descriptorpb.MethodOptions {
	t.Helper()

	for _, rawFile := range rawFiles {
		file := &descriptorpb.FileDescriptorProto{}
		require.NoError(t, proto.Unmarshal(rawFile, file))
		if file.GetName() != filePath {
			continue
		}
		for _, service := range file.GetService() {
			if service.GetName() != serviceName {
				continue
			}
			for _, method := range service.GetMethod() {
				if method.GetName() == methodName {
					return method.GetOptions()
				}
			}
		}
	}

	t.Fatalf("method %s/%s not found in reflected file %s", serviceName, methodName, filePath)
	return nil
}

func reflectedServiceOptions(t *testing.T, rawFiles [][]byte, filePath string, serviceName string) *descriptorpb.ServiceOptions {
	t.Helper()

	for _, rawFile := range rawFiles {
		file := &descriptorpb.FileDescriptorProto{}
		require.NoError(t, proto.Unmarshal(rawFile, file))
		if file.GetName() != filePath {
			continue
		}
		for _, service := range file.GetService() {
			if service.GetName() == serviceName {
				return service.GetOptions()
			}
		}
	}

	t.Fatalf("service %s not found in reflected file %s", serviceName, filePath)
	return nil
}
