package toolcache_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.cadenya.com/mcp-grpc-gateway/internal/discovery"
	"go.cadenya.com/mcp-grpc-gateway/internal/testpb"
	"go.cadenya.com/mcp-grpc-gateway/internal/toolcache"
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

	cache.SetLoader(func(context.Context, grpc.ClientConnInterface, []string) ([]protoreflect.ServiceDescriptor, error) {
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
		Server: toolcache.ServerMetadata{
			Name:         "runtime-gateway",
			Title:        "Runtime Gateway",
			Version:      "1.2.3",
			Instructions: "Runtime instructions.",
			WebsiteURL:   "https://example.com/runtime",
		},
	})
	require.NoError(t, cache.Reload(ctx))
	initial := cache.Current()

	require.NoError(t, cache.Reload(ctx))
	require.NotSame(t, initial, cache.Current())
	require.Equal(t, uint64(2), cache.Version())

	session := connectMCP(t, cache.Current())
	defer session.Close()
	init := session.InitializeResult()
	require.Equal(t, "runtime-gateway", init.ServerInfo.Name)
	require.Equal(t, "Runtime Gateway", init.ServerInfo.Title)
	require.Equal(t, "1.2.3", init.ServerInfo.Version)
	require.Equal(t, "https://example.com/runtime", init.ServerInfo.WebsiteURL)
	require.Equal(t, "Runtime instructions.", init.Instructions)

	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		require.NoError(t, err)
		tools = append(tools, tool)
	}
	require.Len(t, tools, 1)
	require.Equal(t, "greet_user", tools[0].Name)
}

func TestCacheRunRetriesAfterInitialReflectionFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr, stop := startGRPCServer(t)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	var calls atomic.Int32
	cache := toolcache.New(toolcache.Options{
		Conn:    conn,
		Service: "functional.v1.GreeterService",
		Loader: func(ctx context.Context, conn grpc.ClientConnInterface, services []string) ([]protoreflect.ServiceDescriptor, error) {
			if calls.Add(1) == 1 {
				return nil, fmt.Errorf("reflection unavailable")
			}
			return discovery.LoadServices(ctx, conn, services)
		},
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cache.Run(ctx, 10*time.Millisecond)
	}()

	require.Eventually(t, func() bool {
		return cache.Current() != nil && cache.Version() == 1
	}, time.Second, 10*time.Millisecond)

	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
	require.GreaterOrEqual(t, calls.Load(), int32(2))
}

func TestCacheReloadLoadsAllServicesWhenNoFilterIsProvided(t *testing.T) {
	ctx := context.Background()
	addr, stop := startGRPCServer(t)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	cache := toolcache.New(toolcache.Options{
		Conn: conn,
	})
	require.NoError(t, cache.Reload(ctx))

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

func TestCacheReloadLogsToolDiffAcrossReloads(t *testing.T) {
	ctx := context.Background()
	addr, stop := startGRPCServer(t)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	handler := &captureHandler{}
	cache := toolcache.New(toolcache.Options{
		Conn:    conn,
		Service: "functional.v1.GreeterService",
		Logger:  slog.New(handler),
	})

	require.NoError(t, cache.Reload(ctx))
	first := handler.lastReload(t)
	require.Equal(t, []string{"greet_user"}, first["tools_added"])
	require.Empty(t, first["tools_removed"])
	require.Equal(t, int64(0), first["tools_unchanged"])

	require.NoError(t, cache.Reload(ctx))
	second := handler.lastReload(t)
	require.Empty(t, second["tools_added"])
	require.Empty(t, second["tools_removed"])
	require.Equal(t, int64(1), second["tools_unchanged"])
}

// captureHandler is a minimal slog.Handler that records the attributes of every
// "reloaded reflected tools" record so tests can assert on the emitted diff.
type captureHandler struct {
	mu      sync.Mutex
	reloads []map[string]any
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Message != "reloaded reflected tools" {
		return nil
	}
	attrs := map[string]any{}
	record.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	h.reloads = append(h.reloads, attrs)
	h.mu.Unlock()
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler       { return h }

func (h *captureHandler) lastReload(t *testing.T) map[string]any {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	require.NotEmpty(t, h.reloads)
	return h.reloads[len(h.reloads)-1]
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
