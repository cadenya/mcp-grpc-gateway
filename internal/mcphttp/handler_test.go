package mcphttp_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.cadenya.com/mcp-grpc-gateway/internal/mcphttp"
)

func TestHandlerUsesStatelessJSONHTTP(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	handler := mcphttp.NewHandler(provider{server: server}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"test"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", "ignored")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Header().Get("Content-Type"), "application/json")
	require.Empty(t, resp.Header().Get("Mcp-Session-Id"))
}

func TestHandlerRejectsSessionOrientedHTTPMethods(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	handler := mcphttp.NewHandler(provider{server: server}, slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)))

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/mcp", nil)
			req.Header.Set("Accept", "text/event-stream")
			req.Header.Set("Mcp-Session-Id", "ignored")
			resp := httptest.NewRecorder()

			handler.ServeHTTP(resp, req)

			require.Equal(t, http.StatusMethodNotAllowed, resp.Code)
			require.Equal(t, "POST", resp.Header().Get("Allow"))
		})
	}
}

type provider struct {
	server *mcp.Server
}

func (p provider) Current() *mcp.Server {
	return p.server
}
