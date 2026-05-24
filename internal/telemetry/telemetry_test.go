package telemetry_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"go.cadenya.com/mcp-grpc-gateway/internal/telemetry"
	"github.com/stretchr/testify/require"
)

func TestSetupBuildsJSONLoggerAtConfiguredLevel(t *testing.T) {
	var out bytes.Buffer

	logger, shutdown, err := telemetry.Setup(context.Background(), telemetry.Config{
		LogLevel:    "debug",
		LogFormat:   "json",
		ServiceName: "mcp-grpc-gateway",
	}, &out)
	defer shutdown(context.Background())

	require.NoError(t, err)
	require.True(t, logger.Enabled(context.Background(), slog.LevelDebug))
	logger.Info("hello", "component", "test")
	require.Contains(t, out.String(), `"msg":"hello"`)
	require.Contains(t, out.String(), `"component":"test"`)
}

func TestSetupRejectsInvalidLogFormat(t *testing.T) {
	_, _, err := telemetry.Setup(context.Background(), telemetry.Config{
		LogLevel:  "info",
		LogFormat: "pretty",
	}, &bytes.Buffer{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "log format")
}

func TestSetupRejectsInvalidLogLevel(t *testing.T) {
	_, _, err := telemetry.Setup(context.Background(), telemetry.Config{
		LogLevel:  "verbose",
		LogFormat: "text",
	}, &bytes.Buffer{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "log level")
}
