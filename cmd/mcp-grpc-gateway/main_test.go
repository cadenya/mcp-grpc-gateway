package main

import (
	"context"
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
	require.Equal(t, "test.v1.Service", got.service)
	require.Equal(t, "/mcp", got.path)
	require.False(t, got.tls)
	require.Equal(t, time.Minute, got.reloadInterval)
}

func TestCommandRequiresGRPCHostAndService(t *testing.T) {
	cmd := newCommand(func(context.Context, config) error {
		t.Fatal("action should not run for invalid config")
		return nil
	})
	err := cmd.Run(context.Background(), []string{"mcp-grpc-gateway"})

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
