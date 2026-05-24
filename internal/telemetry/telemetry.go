package telemetry

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type Config struct {
	LogLevel     string
	LogFormat    string
	ServiceName  string
	OTELEndpoint string
	OTELInsecure bool
}

func Setup(ctx context.Context, cfg Config, out io.Writer) (*slog.Logger, func(context.Context) error, error) {
	level := new(slog.LevelVar)
	if err := level.UnmarshalText([]byte(defaultString(cfg.LogLevel, "info"))); err != nil {
		return nil, nil, fmt.Errorf("log level: %w", err)
	}

	var handler slog.Handler
	handlerOpts := &slog.HandlerOptions{Level: level}
	switch defaultString(cfg.LogFormat, "text") {
	case "text":
		handler = slog.NewTextHandler(out, handlerOpts)
	case "json":
		handler = slog.NewJSONHandler(out, handlerOpts)
	default:
		return nil, nil, fmt.Errorf("log format must be text or json")
	}

	logger := slog.New(handler)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	shutdown := func(context.Context) error { return nil }
	if cfg.OTELEndpoint != "" {
		exporterOpts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.OTELEndpoint),
		}
		if cfg.OTELInsecure {
			exporterOpts = append(exporterOpts, otlptracegrpc.WithInsecure())
		}
		exporter, err := otlptracegrpc.New(ctx, exporterOpts...)
		if err != nil {
			return nil, nil, fmt.Errorf("create otlp trace exporter: %w", err)
		}
		res := resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(defaultString(cfg.ServiceName, "mcp-grpc-gateway")),
		)
		provider := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(provider)
		shutdown = provider.Shutdown
	}

	return logger, shutdown, nil
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
