package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	fakerpb "go.cadenya.com/mcp-grpc-gateway/examples/faker/fakerpb"
	"go.cadenya.com/mcp-grpc-gateway/internal/mcphttp"
	"go.cadenya.com/mcp-grpc-gateway/internal/toolcache"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func TestFakerServiceListsFiltersAndGeneratesValues(t *testing.T) {
	server := newFakerServer()

	all, err := server.GetFakerOptions(context.Background(), &fakerpb.GetFakerOptionsRequest{})
	require.NoError(t, err)
	require.Greater(t, len(all.GetOptions()), 100)
	require.Contains(t, optionNames(all.GetOptions()), "internet.email")
	require.Contains(t, optionNames(all.GetOptions()), "lorem.sentence")
	require.Contains(t, optionNames(all.GetOptions()), "faker.int_between")

	filtered, err := server.GetFakerOptions(context.Background(), &fakerpb.GetFakerOptionsRequest{Filter: "email"})
	require.NoError(t, err)
	require.Contains(t, optionNames(filtered.GetOptions()), "internet.email")

	generated, err := server.GenerateFake(context.Background(), &fakerpb.GenerateFakeRequest{Name: "internet.email"})
	require.NoError(t, err)
	require.Equal(t, "internet.email", generated.GetName())
	require.Contains(t, generated.GetValue(), "@")
}

func TestFakerServiceExposesArgumentsAndUsesDefaults(t *testing.T) {
	server := newFakerServer()

	all, err := server.GetFakerOptions(context.Background(), &fakerpb.GetFakerOptionsRequest{Filter: "lorem.sentence"})
	require.NoError(t, err)
	require.NotEmpty(t, all.GetOptions())
	option := findOption(t, all.GetOptions(), "lorem.sentence")
	require.Equal(t, "count", option.GetArguments()[0].GetName())
	require.Equal(t, "integer", option.GetArguments()[0].GetType())
	require.Equal(t, float64(8), option.GetArguments()[0].GetDefault().GetNumberValue())

	generated, err := server.GenerateFake(context.Background(), &fakerpb.GenerateFakeRequest{Name: "lorem.sentence"})
	require.NoError(t, err)
	require.Equal(t, "lorem.sentence", generated.GetName())
	require.NotEmpty(t, generated.GetValue())
}

func TestFakerServiceRejectsUnknownFakeName(t *testing.T) {
	_, err := newFakerServer().GenerateFake(context.Background(), &fakerpb.GenerateFakeRequest{Name: "missing.fake"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported faker name")
}

func TestFakerServiceIsExposedThroughGatewayAtFakerPath(t *testing.T) {
	ctx := context.Background()
	addr, stop := startTestFakerGRPC(t)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	cache := toolcache.New(toolcache.Options{
		Conn:    conn,
		Service: "examples.faker.v1.FakerService",
	})
	require.NoError(t, cache.Reload(ctx))

	mux := http.NewServeMux()
	mux.Handle("/mcp/faker", mcphttp.NewHandler(cache, nil))
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	session := connectHTTPMCP(t, httpServer.URL+"/mcp/faker", httpServer.Client())
	defer session.Close()

	var toolNames []string
	for tool, err := range session.Tools(ctx, nil) {
		require.NoError(t, err)
		toolNames = append(toolNames, tool.Name)
	}
	require.ElementsMatch(t, []string{"GetFakerOptions", "GenerateFake"}, toolNames)

	got, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "GenerateFake",
		Arguments: map[string]any{"name": "lorem.sentence", "args": map[string]any{"count": 12}},
	})
	require.NoError(t, err)
	require.False(t, got.IsError)
	content := got.StructuredContent.(map[string]any)
	require.Equal(t, "lorem.sentence", content["name"])
	require.NotEmpty(t, content["value"])
}

func startTestFakerGRPC(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := grpc.NewServer()
	fakerpb.RegisterFakerServiceServer(server, newFakerServer())
	reflection.Register(server)
	go func() {
		_ = server.Serve(listener)
	}()

	return listener.Addr().String(), func() {
		server.Stop()
		_ = listener.Close()
	}
}

func connectHTTPMCP(t *testing.T, endpoint string, httpClient *http.Client) *mcp.ClientSession {
	t.Helper()

	client := mcp.NewClient(&mcp.Implementation{Name: "faker-test-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: httpClient,
	}, nil)
	require.NoError(t, err)
	return session
}

func optionNames(options []*fakerpb.FakerOption) []string {
	names := make([]string, 0, len(options))
	for _, option := range options {
		names = append(names, option.GetName())
	}
	return names
}

func findOption(t *testing.T, options []*fakerpb.FakerOption, name string) *fakerpb.FakerOption {
	t.Helper()
	for _, option := range options {
		if option.GetName() == name {
			return option
		}
	}
	t.Fatalf("option %q not found", name)
	return nil
}
