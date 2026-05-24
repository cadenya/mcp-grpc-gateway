package functional_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	grpcmcpgatewayv1 "go.cadenya.com/mcp-grpc-gateway/gen/grpcmcpgateway/v1"
	"go.cadenya.com/mcp-grpc-gateway/internal/discovery"
	"go.cadenya.com/mcp-grpc-gateway/internal/gateway"
	"go.cadenya.com/mcp-grpc-gateway/internal/mcphttp"
	"go.cadenya.com/mcp-grpc-gateway/internal/testpb"
	"go.cadenya.com/mcp-grpc-gateway/internal/toolcache"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	reflectionv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
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

	httpServer := httptest.NewServer(mcphttp.NewHandler(staticProvider{server: mcpServer}, nil))
	defer httpServer.Close()
	assertStatelessHTTPInitialize(t, httpServer)

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

func TestMCPEndpointKeepsCachedToolsWhenReflectionReloadFails(t *testing.T) {
	ctx := context.Background()
	grpcAddr, stopGRPC := startGreeterGRPCServer(t)
	defer stopGRPC()

	grpcClient, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer grpcClient.Close()

	cache := toolcache.New(toolcache.Options{
		Conn:    grpcClient,
		Service: "functional.v1.GreeterService",
	})
	require.NoError(t, cache.Reload(ctx))
	initial := cache.Current()
	require.NotNil(t, initial)

	httpServer := httptest.NewServer(mcphttp.NewHandler(cache, nil))
	defer httpServer.Close()

	cache.SetLoader(func(context.Context, grpc.ClientConnInterface, []string) ([]protoreflect.ServiceDescriptor, error) {
		return nil, context.DeadlineExceeded
	})
	require.Error(t, cache.Reload(ctx))
	require.Same(t, initial, cache.Current())

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "functional-client", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL,
		HTTPClient:           httpServer.Client(),
		DisableStandaloneSSE: true,
	}, nil)
	require.NoError(t, err)
	defer session.Close()

	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		require.NoError(t, err)
		tools = append(tools, tool)
	}
	require.Len(t, tools, 1)
	require.Equal(t, "greet_user", tools[0].Name)
}

func TestRequireToolAnnotationsStillExposesAnnotatedTools(t *testing.T) {
	ctx := context.Background()
	grpcAddr, stopGRPC := startGreeterGRPCServer(t)
	defer stopGRPC()

	grpcClient, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer grpcClient.Close()

	cache := toolcache.New(toolcache.Options{
		Conn:                   grpcClient,
		Service:                "functional.v1.GreeterService",
		RequireToolAnnotations: true,
	})
	require.NoError(t, cache.Reload(ctx))

	session := connectHTTPMCP(t, cache)
	defer session.Close()

	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		require.NoError(t, err)
		tools = append(tools, tool)
	}
	require.Len(t, tools, 1)
	require.Equal(t, "greet_user", tools[0].Name)
}

func TestForwardHeaderReachesGRPCMetadata(t *testing.T) {
	ctx := context.Background()
	seenAuthorization := make(chan string, 1)
	grpcAddr, stopGRPC := startGreeterGRPCServerWith(t, &greeterServer{seenAuthorization: seenAuthorization})
	defer stopGRPC()

	grpcClient, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer grpcClient.Close()

	cache := toolcache.New(toolcache.Options{
		Conn:    grpcClient,
		Service: "functional.v1.GreeterService",
	})
	require.NoError(t, cache.Reload(ctx))

	httpServer := httptest.NewServer(mcphttp.NewHandler(cache, nil, mcphttp.WithForwardHeaders([]string{"Authorization"})))
	defer httpServer.Close()

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "functional-client", Version: "test"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
		HTTPClient: &http.Client{Transport: headerTransport{
			base: httpServer.Client().Transport,
			key:  "Authorization",
			val:  "Bearer test-token",
		}},
		DisableStandaloneSSE: true,
	}, nil)
	require.NoError(t, err)
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "greet_user",
		Arguments: map[string]any{"name": "Ada"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, "Bearer test-token", <-seenAuthorization)
}

func startGreeterGRPCServer(t *testing.T) (string, func()) {
	t.Helper()

	return startGreeterGRPCServerWith(t, &greeterServer{})
}

func startGreeterGRPCServerWith(t *testing.T, greeter testpb.GreeterServiceServer) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	testpb.RegisterGreeterServiceServer(server, greeter)
	reflection.Register(server)

	go func() {
		_ = server.Serve(listener)
	}()

	return listener.Addr().String(), func() {
		server.Stop()
		_ = listener.Close()
	}
}

func connectHTTPMCP(t *testing.T, cache *toolcache.Cache) *mcp.ClientSession {
	t.Helper()

	httpServer := httptest.NewServer(mcphttp.NewHandler(cache, nil))
	t.Cleanup(httpServer.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "functional-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL,
		HTTPClient:           httpServer.Client(),
		DisableStandaloneSSE: true,
	}, nil)
	require.NoError(t, err)
	return session
}

func assertStatelessHTTPInitialize(t *testing.T, httpServer *httptest.Server) {
	t.Helper()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"functional-client","version":"test"}}}`)
	req, err := http.NewRequest(http.MethodPost, httpServer.URL, body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := httpServer.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	require.Empty(t, resp.Header.Get("Mcp-Session-Id"))
}

type greeterServer struct {
	testpb.UnimplementedGreeterServiceServer
	seenAuthorization chan<- string
}

type staticProvider struct {
	server *mcp.Server
}

func (p staticProvider) Current() *mcp.Server {
	return p.server
}

func (g greeterServer) Greet(ctx context.Context, req *testpb.GreetRequest) (*testpb.GreetResponse, error) {
	if g.seenAuthorization != nil {
		values := metadata.ValueFromIncomingContext(ctx, "authorization")
		if len(values) == 0 {
			g.seenAuthorization <- ""
		} else {
			g.seenAuthorization <- values[0]
		}
	}
	return &testpb.GreetResponse{Greeting: "Hello, " + req.GetName()}, nil
}

type headerTransport struct {
	base http.RoundTripper
	key  string
	val  string
}

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	req = req.Clone(req.Context())
	req.Header.Set(t.key, t.val)
	return base.RoundTrip(req)
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
