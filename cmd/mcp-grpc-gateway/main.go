package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"cadenya.com/mcp-grpc-gateway/internal/mcphttp"
	"cadenya.com/mcp-grpc-gateway/internal/telemetry"
	"cadenya.com/mcp-grpc-gateway/internal/toolcache"
	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type config struct {
	addr                   string
	grpcHost               string
	service                string
	path                   string
	tls                    bool
	reloadInterval         time.Duration
	logLevel               string
	logFormat              string
	otelEndpoint           string
	otelInsecure           bool
	requireToolAnnotations bool
}

func main() {
	if err := newCommand(run).Run(context.Background(), os.Args); err != nil {
		slog.Error("gateway exited", "error", err)
		os.Exit(1)
	}
}

func newCommand(action func(context.Context, config) error) *cli.Command {
	return &cli.Command{
		Name:  "mcp-grpc-gateway",
		Usage: "Expose reflected gRPC unary RPCs as MCP tools",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "addr",
				Value: "0.0.0.0:8080",
				Usage: "HTTP listen address",
			},
			&cli.StringFlag{
				Name:  "grpc-host",
				Usage: "gRPC host to reflect and invoke",
			},
			&cli.StringFlag{
				Name:  "service",
				Usage: "fully-qualified gRPC service name",
			},
			&cli.StringFlag{
				Name:  "path",
				Value: "/mcp",
				Usage: "HTTP path for the MCP endpoint",
			},
			&cli.BoolFlag{
				Name:  "tls",
				Usage: "connect to gRPC using TLS with system roots",
			},
			&cli.DurationFlag{
				Name:  "reload-interval",
				Value: time.Minute,
				Usage: "interval for reloading reflected gRPC tools; set 0 to disable background reloads",
			},
			&cli.StringFlag{
				Name:  "log-level",
				Value: "info",
				Usage: "log level: debug, info, warn, or error",
			},
			&cli.StringFlag{
				Name:  "log-format",
				Value: "text",
				Usage: "log format: text or json",
			},
			&cli.StringFlag{
				Name:  "otel-endpoint",
				Usage: "OTLP gRPC trace endpoint, for example collector:4317; disabled when empty",
			},
			&cli.BoolFlag{
				Name:  "otel-insecure",
				Usage: "disable TLS for the OTLP gRPC trace exporter",
			},
			&cli.BoolFlag{
				Name:  "require-tool-annotations",
				Usage: "only expose RPCs that have grpcmcpgateway.v1.tool annotations",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := configFromCommand(cmd)
			if err != nil {
				return err
			}
			return action(ctx, cfg)
		},
	}
}

func configFromCommand(cmd *cli.Command) (config, error) {
	cfg := config{
		addr:                   cmd.String("addr"),
		grpcHost:               cmd.String("grpc-host"),
		service:                cmd.String("service"),
		path:                   cmd.String("path"),
		tls:                    cmd.Bool("tls"),
		reloadInterval:         cmd.Duration("reload-interval"),
		logLevel:               cmd.String("log-level"),
		logFormat:              cmd.String("log-format"),
		otelEndpoint:           cmd.String("otel-endpoint"),
		otelInsecure:           cmd.Bool("otel-insecure"),
		requireToolAnnotations: cmd.Bool("require-tool-annotations"),
	}
	if cfg.path == "" {
		cfg.path = "/mcp"
	}
	if !strings.HasPrefix(cfg.path, "/") {
		cfg.path = "/" + cfg.path
	}

	var errs []error
	if cfg.grpcHost == "" {
		errs = append(errs, errors.New("--grpc-host is required"))
	}
	if cfg.service == "" {
		errs = append(errs, errors.New("--service is required"))
	}
	return cfg, errors.Join(errs...)
}

func run(ctx context.Context, cfg config) error {
	logger, shutdown, err := telemetry.Setup(ctx, telemetry.Config{
		LogLevel:     cfg.logLevel,
		LogFormat:    cfg.logFormat,
		ServiceName:  "mcp-grpc-gateway",
		OTELEndpoint: cfg.otelEndpoint,
		OTELInsecure: cfg.otelInsecure,
	}, os.Stderr)
	if err != nil {
		return err
	}
	defer func() {
		if err := shutdown(context.Background()); err != nil {
			logger.Error("shutdown telemetry", "error", err)
		}
	}()
	slog.SetDefault(logger)

	conn, err := grpc.NewClient(cfg.grpcHost, dialOptions(cfg)...)
	if err != nil {
		return fmt.Errorf("create grpc client: %w", err)
	}
	defer conn.Close()

	cache := toolcache.New(toolcache.Options{
		Conn:                   conn,
		Service:                cfg.service,
		Logger:                 logger,
		RequireToolAnnotations: cfg.requireToolAnnotations,
	})
	if err := cache.Reload(ctx); err != nil {
		return err
	}
	if cfg.reloadInterval > 0 {
		go func() {
			if err := cache.Run(ctx, cfg.reloadInterval); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("tool cache reload loop stopped", "error", err)
			}
		}()
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.path, mcphttp.NewHandler(cache, logger))

	logger.Info("serving MCP endpoint", "addr", cfg.addr, "path", cfg.path, "grpc_service", cfg.service)
	return http.ListenAndServe(cfg.addr, mux)
}

func dialOptions(cfg config) []grpc.DialOption {
	if cfg.tls {
		return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, ""))}
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
}
