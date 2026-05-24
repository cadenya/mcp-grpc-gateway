package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommandDefaultsAndNormalizesPath(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--service", "test.v1.Service",
		"--path", "mcp",
	})

	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:8080", got.addr)
	require.Equal(t, "localhost:50051", got.grpcHost)
	require.Equal(t, []string{"test.v1.Service"}, got.services)
	require.Equal(t, "/mcp", got.path)
	require.False(t, got.tls)
	require.Equal(t, time.Minute, got.reloadInterval)
	require.Equal(t, "info", got.logLevel)
	require.Equal(t, "text", got.logFormat)
	require.Empty(t, got.otelEndpoint)
	require.False(t, got.requireToolAnnotations)
	require.Empty(t, got.forwardHeaders)
	require.Equal(t, "mcp-grpc-gateway", got.mcpName)
	require.Empty(t, got.mcpTitle)
	require.Equal(t, "dev", got.mcpVersion)
	require.Empty(t, got.mcpInstructions)
	require.Empty(t, got.mcpWebsiteURL)
}

func TestCommandRequiresGRPCHostAndService(t *testing.T) {
	cmd := newCommand(func(context.Context, config) error {
		t.Fatal("action should not run for invalid config")
		return nil
	})
	err := cmd.Run(context.Background(), []string{"mcp-grpc-gateway"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "--grpc-host is required")
}

func TestCommandAllowsOmittingServiceFilter(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
	})

	require.NoError(t, err)
	require.Empty(t, got.services)
}

func TestCommandParsesMultipleServiceFilters(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--service", "test.v1.FirstService",
		"--service", "test.v1.SecondService",
	})

	require.NoError(t, err)
	require.Equal(t, []string{"test.v1.FirstService", "test.v1.SecondService"}, got.services)
}

func TestDialOptionsUseTLSOnlyWhenRequested(t *testing.T) {
	plain := dialOptions(config{})
	secure := dialOptions(config{tls: true})

	require.Len(t, plain, 1)
	require.Len(t, secure, 1)
	require.NotEqual(t, plain[0], secure[0])
}

func TestCommandParsesReloadInterval(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--service", "test.v1.Service",
		"--reload-interval", "5s",
	})

	require.NoError(t, err)
	require.Equal(t, 5*time.Second, got.reloadInterval)
}

func TestCommandParsesTelemetryFlags(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--service", "test.v1.Service",
		"--log-level", "debug",
		"--log-format", "json",
		"--otel-endpoint", "collector:4317",
		"--otel-insecure",
	})

	require.NoError(t, err)
	require.Equal(t, "debug", got.logLevel)
	require.Equal(t, "json", got.logFormat)
	require.Equal(t, "collector:4317", got.otelEndpoint)
	require.True(t, got.otelInsecure)
}

func TestCommandParsesRequireToolAnnotations(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--service", "test.v1.Service",
		"--require-tool-annotations",
	})

	require.NoError(t, err)
	require.True(t, got.requireToolAnnotations)
}

func TestCommandParsesForwardHeaders(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--service", "test.v1.Service",
		"--forward-header", "Authorization",
		"--forward-header", "X-Request-ID",
	})

	require.NoError(t, err)
	require.Equal(t, []string{"Authorization", "X-Request-ID"}, got.forwardHeaders)
}

func TestCommandReadsMCPMetadataFromEnvironment(t *testing.T) {
	t.Setenv("MCP_NAME", "env-gateway")
	t.Setenv("MCP_TITLE", "Env Gateway")
	t.Setenv("MCP_VERSION", "2.0.0")
	t.Setenv("MCP_INSTRUCTIONS", "Use env metadata.")
	t.Setenv("MCP_WEBSITE_URL", "https://example.com/env")

	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
	})

	require.NoError(t, err)
	require.Equal(t, "env-gateway", got.mcpName)
	require.Equal(t, "Env Gateway", got.mcpTitle)
	require.Equal(t, "2.0.0", got.mcpVersion)
	require.Equal(t, "Use env metadata.", got.mcpInstructions)
	require.Equal(t, "https://example.com/env", got.mcpWebsiteURL)
}

func TestCommandMCPMetadataFlagsOverrideEnvironment(t *testing.T) {
	t.Setenv("MCP_NAME", "env-gateway")
	t.Setenv("MCP_TITLE", "Env Gateway")
	t.Setenv("MCP_VERSION", "2.0.0")
	t.Setenv("MCP_INSTRUCTIONS", "Use env metadata.")
	t.Setenv("MCP_WEBSITE_URL", "https://example.com/env")

	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--mcp-name", "flag-gateway",
		"--mcp-title", "Flag Gateway",
		"--mcp-version", "3.0.0",
		"--mcp-instructions", "Use flag metadata.",
		"--mcp-website-url", "https://example.com/flag",
	})

	require.NoError(t, err)
	require.Equal(t, "flag-gateway", got.mcpName)
	require.Equal(t, "Flag Gateway", got.mcpTitle)
	require.Equal(t, "3.0.0", got.mcpVersion)
	require.Equal(t, "Use flag metadata.", got.mcpInstructions)
	require.Equal(t, "https://example.com/flag", got.mcpWebsiteURL)
}

func TestServeHTTPShutsDownWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	server := &http.Server{
		Handler: mux,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveHTTP(ctx, server, listener, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	resp, err := http.Get("http://" + listener.Addr().String() + "/health")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	cancel()

	require.NoError(t, <-errCh)
}
