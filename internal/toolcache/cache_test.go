package toolcache_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	"cadenya.com/mcp-grpc-gateway/internal/testpb"
	"cadenya.com/mcp-grpc-gateway/internal/toolcache"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCacheReloadKeepsLastKnownGoodServerOnReflectionFailure(t *testing.T) {
	ctx := context.Background()
	addr, stop := startGRPCServer(t)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	cache := toolcache.New(toolcache.Options{
		Conn:    conn,
		Service: "functional.v1.GreeterService",
	})
	require.NoError(t, cache.Reload(ctx))
	initial := cache.Current()
	require.NotNil(t, initial)
	require.Equal(t, uint64(1), cache.Version())

	cache.SetLoader(func(context.Context, grpc.ClientConnInterface, string) (protoreflect.ServiceDescriptor, error) {
		return nil, fmt.Errorf("reflection unavailable")
	})

	require.Error(t, cache.Reload(ctx))
	require.Same(t, initial, cache.Current())
	require.Equal(t, uint64(1), cache.Version())
}

func TestCacheReloadSwapsServerAfterSuccessfulReflectionReload(t *testing.T) {
	ctx := context.Background()
	addr, stop := startGRPCServer(t)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	cache := toolcache.New(toolcache.Options{
		Conn:    conn,
		Service: "functional.v1.GreeterService",
	})
	require.NoError(t, cache.Reload(ctx))
	initial := cache.Current()

	require.NoError(t, cache.Reload(ctx))
	require.NotSame(t, initial, cache.Current())
	require.Equal(t, uint64(2), cache.Version())

	session := connectMCP(t, cache.Current())
	defer session.Close()
	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		require.NoError(t, err)
		tools = append(tools, tool)
	}
	require.Len(t, tools, 1)
	require.Equal(t, "greet_user", tools[0].Name)
}

func startGRPCServer(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	testpb.RegisterGreeterServiceServer(server, greeterServer{})
	reflection.Register(server)
	go func() {
		_ = server.Serve(listener)
	}()

	return listener.Addr().String(), func() {
		server.Stop()
		_ = listener.Close()
	}
}

func connectMCP(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()

	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	_, err := server.Connect(ctx, t1, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	require.NoError(t, err)
	return session
}

type greeterServer struct {
	testpb.UnimplementedGreeterServiceServer
}

func (greeterServer) Greet(_ context.Context, req *testpb.GreetRequest) (*testpb.GreetResponse, error) {
	return &testpb.GreetResponse{Greeting: "Hello, " + req.GetName()}, nil
}
