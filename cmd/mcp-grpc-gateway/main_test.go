package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseConfigDefaultsAndNormalizesPath(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--grpc-host", "localhost:50051",
		"--service", "test.v1.Service",
		"--path", "mcp",
	})

	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:8080", cfg.addr)
	require.Equal(t, "localhost:50051", cfg.grpcHost)
	require.Equal(t, "test.v1.Service", cfg.service)
	require.Equal(t, "/mcp", cfg.path)
	require.False(t, cfg.tls)
}

func TestParseConfigRequiresGRPCHostAndService(t *testing.T) {
	_, err := parseConfig(nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "--grpc-host is required")
	require.Contains(t, err.Error(), "--service is required")
}

func TestDialOptionsUseTLSOnlyWhenRequested(t *testing.T) {
	plain := dialOptions(config{})
	secure := dialOptions(config{tls: true})

	require.Len(t, plain, 1)
	require.Len(t, secure, 1)
	require.NotEqual(t, plain[0], secure[0])
}
