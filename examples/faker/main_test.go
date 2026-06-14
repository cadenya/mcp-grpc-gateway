package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	fakerpb "go.cadenya.com/mcp-grpc-gateway/examples/faker/fakerpb"
	"go.cadenya.com/mcp-grpc-gateway/internal/mcpjson"
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
	mux.Handle("/mcp/faker", mcpjson.NewHandler(cache, nil))
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	toolsBody := postMCP(t, httpServer.URL+"/mcp/faker", httpServer.Client(), "tools/list", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	var toolsResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(toolsBody), &toolsResp))

	var toolNames []string
	for _, tool := range toolsResp.Result.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	require.ElementsMatch(t, []string{"GetFakerOptions", "GenerateFake"}, toolNames)

	callBody := postMCP(t, httpServer.URL+"/mcp/faker", httpServer.Client(), "tools/call", "GenerateFake", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"GenerateFake","arguments":{"name":"lorem.sentence","args":{"count":12}}}}`)
	var callResp struct {
		Result struct {
			StructuredContent map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(callBody), &callResp))
	require.Equal(t, "lorem.sentence", callResp.Result.StructuredContent["name"])
	require.NotEmpty(t, callResp.Result.StructuredContent["value"])
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

func postMCP(t *testing.T, endpoint string, httpClient *http.Client, method string, name string, body string) string {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(raw))
	return string(raw)
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
