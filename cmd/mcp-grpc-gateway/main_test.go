package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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
	require.Equal(t, "127.0.0.1:8080", got.addr)
	require.Equal(t, "localhost:50051", got.grpcHost)
	require.Equal(t, []string{"test.v1.Service"}, got.services)
	require.Equal(t, "/mcp", got.path)
	require.False(t, got.tls)
	require.Empty(t, got.grpcCAFile)
	require.Empty(t, got.grpcClientCertFile)
	require.Empty(t, got.grpcClientKeyFile)
	require.Empty(t, got.grpcServerName)
	require.Equal(t, time.Minute, got.reloadInterval)
	require.Equal(t, 30*time.Second, got.toolCallTimeout)
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

func TestCommandParsesToolCallTimeout(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--tool-call-timeout", "5s",
	})

	require.NoError(t, err)
	require.Equal(t, 5*time.Second, got.toolCallTimeout)
}

func TestCommandDefaultsHealthPath(t *testing.T) {
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
	require.Equal(t, "/health", got.healthPath)
}

func TestCommandParsesAndNormalizesHealthPath(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--health-path", "readyz",
	})

	require.NoError(t, err)
	require.Equal(t, "/readyz", got.healthPath)
}

func TestCommandReadsHealthPathFromEnvironment(t *testing.T) {
	t.Setenv("HEALTH_PATH", "/livez")

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
	require.Equal(t, "/livez", got.healthPath)
}

func TestCommandRejectsMatchingMCPAndHealthPaths(t *testing.T) {
	cmd := newCommand(func(context.Context, config) error {
		t.Fatal("action should not run for invalid config")
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--path", "/readyz",
		"--health-path", "readyz",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "--path and --health-path must be different")
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
	plain, err := dialOptions(config{})
	require.NoError(t, err)
	secure, err := dialOptions(config{tls: true})
	require.NoError(t, err)

	require.Len(t, plain, 2)
	require.Len(t, secure, 2)
	require.NotEqual(t, plain[0], secure[0])
}

func TestCommandParsesGRPCTLSConfigFromEnvironment(t *testing.T) {
	t.Setenv("GRPC_TLS", "true")
	t.Setenv("GRPC_CA_FILE", "/certs/ca.pem")
	t.Setenv("GRPC_CLIENT_CERT_FILE", "/certs/client.crt")
	t.Setenv("GRPC_CLIENT_KEY_FILE", "/certs/client.key")
	t.Setenv("GRPC_SERVER_NAME", "grpc.internal")

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
	require.True(t, got.tls)
	require.Equal(t, "/certs/ca.pem", got.grpcCAFile)
	require.Equal(t, "/certs/client.crt", got.grpcClientCertFile)
	require.Equal(t, "/certs/client.key", got.grpcClientKeyFile)
	require.Equal(t, "grpc.internal", got.grpcServerName)
}

func TestCommandParsesGRPCTLSConfigFromFlags(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--grpc-tls",
		"--grpc-ca-file", "/certs/ca.pem",
		"--grpc-client-cert-file", "/certs/client.crt",
		"--grpc-client-key-file", "/certs/client.key",
		"--grpc-server-name", "grpc.internal",
	})

	require.NoError(t, err)
	require.True(t, got.tls)
	require.Equal(t, "/certs/ca.pem", got.grpcCAFile)
	require.Equal(t, "/certs/client.crt", got.grpcClientCertFile)
	require.Equal(t, "/certs/client.key", got.grpcClientKeyFile)
	require.Equal(t, "grpc.internal", got.grpcServerName)
}

func TestGRPCTLSConfigRequiresClientCertificatePair(t *testing.T) {
	_, err := grpcTLSConfig(config{tls: true, grpcClientCertFile: "/certs/client.crt"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "both --grpc-client-cert-file and --grpc-client-key-file")
}

func TestGRPCTLSConfigLoadsCustomCAClientCertificateAndServerName(t *testing.T) {
	caFile, certFile, keyFile := writeTestCertificates(t)

	tlsConfig, err := grpcTLSConfig(config{
		tls:                true,
		grpcCAFile:         caFile,
		grpcClientCertFile: certFile,
		grpcClientKeyFile:  keyFile,
		grpcServerName:     "grpc.internal",
	})

	require.NoError(t, err)
	require.NotNil(t, tlsConfig.RootCAs)
	require.NotEmpty(t, tlsConfig.RootCAs.Subjects())
	require.Len(t, tlsConfig.Certificates, 1)
	require.Equal(t, "grpc.internal", tlsConfig.ServerName)
}

func TestCommandParsesProtoDescriptor(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--proto-descriptor", "/path/to/descriptor.pb",
	})

	require.NoError(t, err)
	require.Equal(t, "/path/to/descriptor.pb", got.protoDescriptor)
}

func TestCommandParsesProtoDescriptorFromEnvironment(t *testing.T) {
	t.Setenv("PROTO_DESCRIPTOR", "/env/path/descriptor.pb")

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
	require.Equal(t, "/env/path/descriptor.pb", got.protoDescriptor)
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

func TestHealthHandlerReturnsOK(t *testing.T) {
	mux := http.NewServeMux()
	registerHealthHandler(mux, "/readyz")

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp := httptest.NewRecorder()

	mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, "ok", resp.Body.String())
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

func TestRunServesHealthWhenInitialGRPCReflectionIsUnavailable(t *testing.T) {
	addr := freeTCPAddr(t)
	grpcAddr := freeTCPAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx, config{
			addr:            addr,
			grpcHost:        grpcAddr,
			healthPath:      "/health",
			path:            "/mcp",
			reloadInterval:  10 * time.Millisecond,
			toolCallTimeout: 30 * time.Second,
			logLevel:        "error",
			logFormat:       "text",
			mcpName:         "mcp-grpc-gateway",
			mcpVersion:      "test",
		})
	}()

	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Second, 10*time.Millisecond)

	cancel()
	require.NoError(t, <-errCh)
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().String()
}

func writeTestCertificates(t *testing.T) (string, string, string) {
	t.Helper()

	dir := t.TempDir()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caTemplate, &clientKey.PublicKey, caKey)
	require.NoError(t, err)

	caFile := dir + "/ca.pem"
	certFile := dir + "/client.crt"
	keyFile := dir + "/client.key"
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600))
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER}), 0o600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)}), 0o600))
	return caFile, certFile, keyFile
}
