package functional_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	grpcmcpgatewayv1 "go.cadenya.com/mcp-grpc-gateway/gen/grpcmcpgateway/v1"
	"go.cadenya.com/mcp-grpc-gateway/internal/mcpjson"
	"go.cadenya.com/mcp-grpc-gateway/internal/testpb"
	"go.cadenya.com/mcp-grpc-gateway/internal/toolcache"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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

	cache := toolcache.New(toolcache.Options{
		Conn:    grpcClient,
		Service: "functional.v1.GreeterService",
		Server: toolcache.ServerMetadata{
			Name:         "runtime-gateway",
			Title:        "Runtime Gateway",
			Version:      "2.0.0",
			Instructions: "Use runtime metadata.",
			WebsiteURL:   "https://example.com/runtime",
		},
	})
	require.NoError(t, cache.Reload(ctx))

	httpServer := httptest.NewServer(mcpjson.NewHandler(cache, nil))
	defer httpServer.Close()

	discover := postMCP(t, httpServer, "server/discover", "", nil, `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`)
	require.Equal(t, http.StatusOK, discover.StatusCode)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2026-07-28","serverInfo":{"name":"runtime-gateway","title":"Runtime Gateway","version":"2.0.0","websiteUrl":"https://example.com/runtime"},"capabilities":{"tools":{}},"instructions":"Use runtime metadata."}}`, discover.Body)

	tools := postMCP(t, httpServer, "tools/list", "", map[string]string{"Accept": "application/json"}, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	require.Equal(t, http.StatusOK, tools.StatusCode)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"greet_user","description":"Greets a user by name","inputSchema":{"type":"object","properties":{"name":{"type":"string"}}}}],"ttlMs":0,"cacheScope":"public"}}`, tools.Body)

	call := postMCP(t, httpServer, "tools/call", "greet_user", nil, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"greet_user","arguments":{"name":"Ada"}}}`)
	require.Equal(t, http.StatusOK, call.StatusCode)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"{\"greeting\":\"Hello, Ada\"}"}],"structuredContent":{"greeting":"Hello, Ada"}}}`, call.Body)
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

	httpServer := httptest.NewServer(mcpjson.NewHandler(cache, nil))
	defer httpServer.Close()

	cache.SetLoader(func(context.Context, grpc.ClientConnInterface, []string) ([]protoreflect.ServiceDescriptor, error) {
		return nil, context.DeadlineExceeded
	})
	require.Error(t, cache.Reload(ctx))
	require.Same(t, initial, cache.Current())

	tools := postMCP(t, httpServer, "tools/list", "", nil, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	require.Equal(t, http.StatusOK, tools.StatusCode)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"greet_user","description":"Greets a user by name","inputSchema":{"type":"object","properties":{"name":{"type":"string"}}}}],"ttlMs":0,"cacheScope":"public"}}`, tools.Body)
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

	httpServer := httptest.NewServer(mcpjson.NewHandler(cache, nil))
	defer httpServer.Close()

	tools := postMCP(t, httpServer, "tools/list", "", nil, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	require.Equal(t, http.StatusOK, tools.StatusCode)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"greet_user","description":"Greets a user by name","inputSchema":{"type":"object","properties":{"name":{"type":"string"}}}}],"ttlMs":0,"cacheScope":"public"}}`, tools.Body)
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

	httpServer := httptest.NewServer(mcpjson.NewHandler(cache, nil, mcpjson.WithForwardHeaders([]string{"Authorization"})))
	defer httpServer.Close()

	resp := postMCP(t, httpServer, "tools/call", "greet_user", map[string]string{"Authorization": "Bearer test-token"}, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"greet_user","arguments":{"name":"Ada"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "Bearer test-token", <-seenAuthorization)
}

func TestRawHTTPToolCallReturnsOKWithResultBody(t *testing.T) {
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

	httpServer := httptest.NewServer(mcpjson.NewHandler(cache, nil))
	defer httpServer.Close()

	resp := postMCP(t, httpServer, "tools/call", "greet_user", map[string]string{"Accept": "application/json"}, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"greet_user","arguments":{"name":"Ada"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, resp.Body)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"{\"greeting\":\"Hello, Ada\"}"}],"structuredContent":{"greeting":"Hello, Ada"}}}`, resp.Body)

	mismatch := postMCP(t, httpServer, "tools/call", "wrong_tool", nil, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"greet_user","arguments":{"name":"Ada"}}}`)
	require.Equal(t, http.StatusBadRequest, mismatch.StatusCode)
	require.Contains(t, mismatch.Body, "Mcp-Name header value")
}

func TestTraceContextPropagatesFromMCPHTTPToGRPCMetadata(t *testing.T) {
	oldPropagator := otel.GetTextMapPropagator()
	oldTracerProvider := otel.GetTracerProvider()
	tracerProvider := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() {
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
		otel.SetTracerProvider(oldTracerProvider)
		otel.SetTextMapPropagator(oldPropagator)
	})

	ctx := context.Background()
	seenTraceparent := make(chan string, 1)
	grpcAddr, stopGRPC := startGreeterGRPCServerWith(t, &greeterServer{seenTraceparent: seenTraceparent})
	defer stopGRPC()

	grpcClient, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	require.NoError(t, err)
	defer grpcClient.Close()

	cache := toolcache.New(toolcache.Options{
		Conn:    grpcClient,
		Service: "functional.v1.GreeterService",
	})
	require.NoError(t, cache.Reload(ctx))

	httpServer := httptest.NewServer(mcpjson.NewHandler(cache, nil))
	defer httpServer.Close()

	resp := postMCP(t, httpServer, "tools/call", "greet_user", map[string]string{"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"greet_user","arguments":{"name":"Ada"}}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	traceparent := <-seenTraceparent
	require.Contains(t, traceparent, "00-4bf92f3577b34da6a3ce929d0e0e4736-")
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

type mcpResponse struct {
	StatusCode int
	Body       string
}

func postMCP(t *testing.T, httpServer *httptest.Server, method string, name string, headers map[string]string, body string) mcpResponse {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL, bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := httpServer.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return mcpResponse{
		StatusCode: resp.StatusCode,
		Body:       string(raw),
	}
}

type greeterServer struct {
	testpb.UnimplementedGreeterServiceServer
	seenAuthorization chan<- string
	seenTraceparent   chan<- string
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
	if g.seenTraceparent != nil {
		values := metadata.ValueFromIncomingContext(ctx, "traceparent")
		if len(values) == 0 {
			g.seenTraceparent <- ""
		} else {
			g.seenTraceparent <- values[0]
		}
	}
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
