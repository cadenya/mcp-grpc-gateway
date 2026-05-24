package mcphttp

import (
	"log/slog"
	"net/http"
	"strings"

	"go.cadenya.com/mcp-grpc-gateway/internal/forwardmetadata"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc/metadata"
)

type ServerProvider interface {
	Current() *mcp.Server
}

type HandlerOption func(*handlerConfig)

type handlerConfig struct {
	forwardHeaders []string
}

func WithForwardHeaders(headers []string) HandlerOption {
	return func(cfg *handlerConfig) {
		cfg.forwardHeaders = append(cfg.forwardHeaders, headers...)
	}
}

func NewHandler(provider ServerProvider, logger *slog.Logger, opts ...HandlerOption) http.Handler {
	cfg := handlerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return provider.Current()
	}, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		Logger:       logger,
	})
	return forwardHeaderHandler{
		next:    handler,
		headers: normalizedHeaders(cfg.forwardHeaders),
	}
}

type forwardHeaderHandler struct {
	next    http.Handler
	headers []string
}

func (h forwardHeaderHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if len(h.headers) == 0 {
		h.next.ServeHTTP(w, req)
		return
	}
	md := metadata.MD{}
	for _, header := range h.headers {
		for _, value := range req.Header.Values(header) {
			if value == "" {
				continue
			}
			md.Append(strings.ToLower(header), value)
		}
	}
	if len(md) > 0 {
		req = req.WithContext(forwardmetadata.NewContext(req.Context(), md))
	}
	h.next.ServeHTTP(w, req)
}

func normalizedHeaders(headers []string) []string {
	out := make([]string, 0, len(headers))
	seen := map[string]struct{}{}
	for _, header := range headers {
		header = strings.TrimSpace(header)
		if header == "" {
			continue
		}
		key := strings.ToLower(header)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, header)
	}
	return out
}
