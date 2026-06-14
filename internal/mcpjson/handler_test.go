package mcpjson_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.cadenya.com/mcp-grpc-gateway/internal/mcpjson"
	"go.cadenya.com/mcp-grpc-gateway/internal/testpb"
	"go.cadenya.com/mcp-grpc-gateway/internal/toolregistry"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestToolsListDoesNotRequireEventStreamAccept(t *testing.T) {
	handler := newTestHandler(t)
	resp := postJSON(t, handler, map[string]string{
		"MCP-Protocol-Version": "2026-07-28",
		"Mcp-Method":           "tools/list",
		"Accept":               "application/json",
	}, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Equal(t, "application/json", resp.Header().Get("Content-Type"))
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"description":"Greets a user by name","inputSchema":{"properties":{"name":{"type":"string"}},"type":"object"},"name":"greet_user"}],"ttlMs":0,"cacheScope":"public"}}`, resp.Body.String())
}

func TestToolCallReturnsStructuredContent(t *testing.T) {
	handler := newTestHandler(t)
	resp := postJSON(t, handler, map[string]string{
		"MCP-Protocol-Version": "2026-07-28",
		"Mcp-Method":           "tools/call",
		"Mcp-Name":             "greet_user",
	}, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"greet_user","arguments":{"name":"Ada"}}}`)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.JSONEq(t, `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"{\"greeting\":\"Hello, Ada\"}"}],"structuredContent":{"greeting":"Hello, Ada"}}}`, resp.Body.String())
}

func TestToolCallHeaderNameMustMatchBodyName(t *testing.T) {
	handler := newTestHandler(t)
	resp := postJSON(t, handler, map[string]string{
		"MCP-Protocol-Version": "2026-07-28",
		"Mcp-Method":           "tools/call",
		"Mcp-Name":             "wrong_name",
	}, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"greet_user","arguments":{"name":"Ada"}}}`)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "Mcp-Name header value")
}

func TestRejectsOldProtocolVersion(t *testing.T) {
	handler := newTestHandler(t)
	resp := postJSON(t, handler, map[string]string{
		"MCP-Protocol-Version": "2025-06-18",
		"Mcp-Method":           "tools/list",
	}, `{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{}}`)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "unsupported MCP protocol version")
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	service := testpb.File_functional_v1_greeter_proto.Services().ByName("GreeterService")
	registry, err := toolregistry.Build(toolregistry.BuildOptions{
		Conn:                    &fakeConn{method: service.Methods().ByName("Greet")},
		Services:                []protoreflect.ServiceDescriptor{service},
		Logger:                  slog.Default(),
		RegisteredToolNameOwner: map[string]string{},
	})
	require.NoError(t, err)
	return mcpjson.NewHandler(provider{registry: registry}, slog.Default())
}

func postJSON(t *testing.T, handler http.Handler, headers map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if !json.Valid(resp.Body.Bytes()) {
		t.Fatalf("response body is not valid JSON: %q", resp.Body.String())
	}
	return resp
}

type provider struct {
	registry *toolregistry.Registry
}

func (p provider) Current() *toolregistry.Registry {
	return p.registry
}

type fakeConn struct {
	method protoreflect.MethodDescriptor
}

func (f *fakeConn) Invoke(ctx context.Context, method string, args any, reply any, _ ...grpc.CallOption) error {
	req := args.(proto.Message).ProtoReflect()
	resp := reply.(*dynamicpb.Message)
	resp.Set(f.method.Output().Fields().ByName("greeting"), protoreflect.ValueOfString("Hello, "+req.Get(f.method.Input().Fields().ByName("name")).String()))
	return nil
}

func (f *fakeConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, fmt.Errorf("streaming is not supported")
}
