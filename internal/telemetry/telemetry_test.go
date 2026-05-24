package telemetry_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.cadenya.com/mcp-grpc-gateway/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
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

func TestSetupConfiguresTraceContextAndBaggagePropagators(t *testing.T) {
	oldPropagator := otel.GetTextMapPropagator()
	defer otel.SetTextMapPropagator(oldPropagator)

	_, shutdown, err := telemetry.Setup(context.Background(), telemetry.Config{
		LogLevel:  "info",
		LogFormat: "text",
	}, &bytes.Buffer{})
	defer shutdown(context.Background())
	require.NoError(t, err)

	ctx := otel.GetTextMapPropagator().Extract(context.Background(), propagation.MapCarrier{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"baggage":     "workspace=demo",
	})

	spanContext := trace.SpanContextFromContext(ctx)
	require.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", spanContext.TraceID().String())
	require.Equal(t, "00f067aa0ba902b7", spanContext.SpanID().String())
	member := baggage.FromContext(ctx).Member("workspace")
	require.Equal(t, "demo", member.Value())
}
