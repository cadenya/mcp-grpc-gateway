package mcpjson

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/sourcegraph/jsonrpc2"
	"go.cadenya.com/mcp-grpc-gateway/internal/forwardmetadata"
	"go.cadenya.com/mcp-grpc-gateway/internal/toolregistry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc/metadata"
)

const ProtocolVersion = "2026-07-28"

type RegistryProvider interface {
	Current() *toolregistry.Registry
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

type Handler struct {
	provider RegistryProvider
	logger   *slog.Logger
	headers  []string
	methods  map[string]methodHandler
}

type methodHandler func(*http.Request, *jsonrpc2.Request, *toolregistry.Registry) (any, *jsonrpc2.Error)

func NewHandler(provider RegistryProvider, logger *slog.Logger, opts ...HandlerOption) http.Handler {
	cfg := handlerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{
		provider: provider,
		logger:   logger,
		headers:  normalizedHeaders(cfg.forwardHeaders),
	}
	h.methods = map[string]methodHandler{
		"server/discover": h.handleServerDiscover,
		"tools/list":      h.handleToolsList,
		"tools/call":      h.handleToolsCall,
	}
	return otelhttp.NewHandler(h, "mcp.http")
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(errorResponse(jsonrpc2.ID{}, jsonrpc2.CodeInvalidRequest, "method not allowed"))
		return
	}
	if mediaType(req.Header.Get("Content-Type")) != "application/json" {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		_ = json.NewEncoder(w).Encode(errorResponse(jsonrpc2.ID{}, jsonrpc2.CodeInvalidRequest, "Content-Type must be application/json"))
		return
	}
	if req.Header.Get("MCP-Protocol-Version") != ProtocolVersion {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse(jsonrpc2.ID{}, jsonrpc2.CodeInvalidRequest, "unsupported MCP protocol version"))
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse(jsonrpc2.ID{}, jsonrpc2.CodeInvalidRequest, "failed to read request body"))
		return
	}
	var rpcReq jsonrpc2.Request
	if err := json.Unmarshal(body, &rpcReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse(jsonrpc2.ID{}, jsonrpc2.CodeParseError, "malformed JSON-RPC request"))
		return
	}
	if err := validateHeaders(req.Header, &rpcReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse(rpcReq.ID, jsonrpc2.CodeInvalidRequest, err.Error()))
		return
	}

	registry := h.provider.Current()
	if registry == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(errorResponse(rpcReq.ID, jsonrpc2.CodeInternalError, "no tool registry available"))
		return
	}
	if len(h.headers) > 0 {
		req = req.WithContext(forwardmetadata.NewContext(req.Context(), forwardedMetadata(req.Header, h.headers)))
	}
	handler, ok := h.methods[rpcReq.Method]
	if !ok {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(errorResponse(rpcReq.ID, jsonrpc2.CodeMethodNotFound, "method not found"))
		return
	}
	result, rpcErr := handler(req, &rpcReq, registry)
	if rpcErr != nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(&jsonrpc2.Response{ID: rpcReq.ID, Error: rpcErr})
		return
	}
	resp := &jsonrpc2.Response{ID: rpcReq.ID}
	if err := resp.SetResult(result); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse(rpcReq.ID, jsonrpc2.CodeInternalError, "failed to encode JSON-RPC result"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleServerDiscover(_ *http.Request, _ *jsonrpc2.Request, registry *toolregistry.Registry) (any, *jsonrpc2.Error) {
	server := registry.Server()
	return map[string]any{
		"protocolVersion": ProtocolVersion,
		"serverInfo": map[string]any{
			"name":       server.Name,
			"title":      server.Title,
			"version":    server.Version,
			"websiteUrl": server.WebsiteURL,
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"instructions": server.Instructions,
	}, nil
}

func (h *Handler) handleToolsList(_ *http.Request, _ *jsonrpc2.Request, registry *toolregistry.Registry) (any, *jsonrpc2.Error) {
	return map[string]any{
		"tools":      registry.Tools(),
		"ttlMs":      0,
		"cacheScope": "public",
	}, nil
}

func (h *Handler) handleToolsCall(req *http.Request, rpcReq *jsonrpc2.Request, registry *toolregistry.Registry) (any, *jsonrpc2.Error) {
	var params callToolParams
	if rpcReq.Params == nil {
		return nil, rpcError(jsonrpc2.CodeInvalidParams, "tools/call params are required")
	}
	if err := json.Unmarshal(*rpcReq.Params, &params); err != nil {
		return nil, rpcError(jsonrpc2.CodeInvalidParams, "invalid tools/call params")
	}
	if strings.TrimSpace(params.Name) == "" {
		return nil, rpcError(jsonrpc2.CodeInvalidParams, "tools/call params.name is required")
	}
	result, err := registry.Call(req.Context(), params.Name, params.Arguments)
	if err != nil {
		return nil, rpcError(jsonrpc2.CodeInvalidParams, err.Error())
	}
	return result, nil
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func validateHeaders(header http.Header, req *jsonrpc2.Request) error {
	method := header.Get("Mcp-Method")
	if method == "" {
		return fmt.Errorf("missing required Mcp-Method header")
	}
	if method != req.Method {
		return fmt.Errorf("Mcp-Method header value %q does not match body value %q", method, req.Method)
	}
	if req.Method != "tools/call" {
		return nil
	}
	name := header.Get("Mcp-Name")
	if name == "" {
		return fmt.Errorf("missing required Mcp-Name header for method %q", req.Method)
	}
	if req.Params == nil {
		return fmt.Errorf("tools/call params are required")
	}
	var params callToolParams
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return fmt.Errorf("invalid tools/call params")
	}
	if name != params.Name {
		return fmt.Errorf("Mcp-Name header value %q does not match body value %q", name, params.Name)
	}
	return nil
}

func errorResponse(id jsonrpc2.ID, code int64, message string) *jsonrpc2.Response {
	return &jsonrpc2.Response{ID: id, Error: rpcError(code, message)}
}

func rpcError(code int64, message string) *jsonrpc2.Error {
	return &jsonrpc2.Error{Code: code, Message: message}
}

func mediaType(value string) string {
	if value == "" {
		return ""
	}
	mt, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return mt
}

func forwardedMetadata(header http.Header, headers []string) metadata.MD {
	md := metadata.MD{}
	for _, name := range headers {
		for _, value := range header.Values(name) {
			if value == "" {
				continue
			}
			md.Append(strings.ToLower(name), value)
		}
	}
	return md
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
