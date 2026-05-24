package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"go.cadenya.com/mcp-grpc-gateway/internal/mcphttp"
	"go.cadenya.com/mcp-grpc-gateway/internal/telemetry"
	"go.cadenya.com/mcp-grpc-gateway/internal/toolcache"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type config struct {
	addr                   string
	grpcHost               string
	services               []string
	path                   string
	tls                    bool
	reloadInterval         time.Duration
	logLevel               string
	logFormat              string
	otelEndpoint           string
	otelInsecure           bool
	requireToolAnnotations bool
	forwardHeaders         []string
	mcpName                string
	mcpTitle               string
	mcpVersion             string
	mcpInstructions        string
	mcpWebsiteURL          string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := newCommand(run).Run(ctx, os.Args); err != nil {
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
			&cli.StringSliceFlag{
				Name:  "service",
				Usage: "fully-qualified gRPC service name to expose; may be repeated; omit to expose all reflected services",
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
			&cli.StringSliceFlag{
				Name:  "forward-header",
				Usage: "HTTP header to forward as gRPC metadata on tool calls; may be repeated",
			},
			&cli.StringFlag{
				Name:    "mcp-name",
				Value:   "mcp-grpc-gateway",
				Usage:   "MCP server implementation name",
				Sources: cli.EnvVars("MCP_NAME"),
			},
			&cli.StringFlag{
				Name:    "mcp-title",
				Usage:   "MCP server display title",
				Sources: cli.EnvVars("MCP_TITLE"),
			},
			&cli.StringFlag{
				Name:    "mcp-version",
				Value:   "dev",
				Usage:   "MCP server implementation version",
				Sources: cli.EnvVars("MCP_VERSION"),
			},
			&cli.StringFlag{
				Name:    "mcp-instructions",
				Usage:   "MCP server instructions returned during initialize",
				Sources: cli.EnvVars("MCP_INSTRUCTIONS"),
			},
			&cli.StringFlag{
				Name:    "mcp-website-url",
				Usage:   "MCP server website URL returned during initialize",
				Sources: cli.EnvVars("MCP_WEBSITE_URL"),
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
		services:               cmd.StringSlice("service"),
		path:                   cmd.String("path"),
		tls:                    cmd.Bool("tls"),
		reloadInterval:         cmd.Duration("reload-interval"),
		logLevel:               cmd.String("log-level"),
		logFormat:              cmd.String("log-format"),
		otelEndpoint:           cmd.String("otel-endpoint"),
		otelInsecure:           cmd.Bool("otel-insecure"),
		requireToolAnnotations: cmd.Bool("require-tool-annotations"),
		forwardHeaders:         cmd.StringSlice("forward-header"),
		mcpName:                cmd.String("mcp-name"),
		mcpTitle:               cmd.String("mcp-title"),
		mcpVersion:             cmd.String("mcp-version"),
		mcpInstructions:        cmd.String("mcp-instructions"),
		mcpWebsiteURL:          cmd.String("mcp-website-url"),
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
		Conn:     conn,
		Services: cfg.services,
		Server: toolcache.ServerMetadata{
			Name:         cfg.mcpName,
			Title:        cfg.mcpTitle,
			Version:      cfg.mcpVersion,
			Instructions: cfg.mcpInstructions,
			WebsiteURL:   cfg.mcpWebsiteURL,
		},
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
	mux.Handle(cfg.path, mcphttp.NewHandler(cache, logger, mcphttp.WithForwardHeaders(cfg.forwardHeaders)))

	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.addr, err)
	}
	server := &http.Server{
		Handler: mux,
	}

	logger.Info("serving MCP endpoint", "addr", listener.Addr().String(), "path", cfg.path, "grpc_services", cfg.services)
	return serveHTTP(ctx, server, listener, logger)
}

func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		logger.Info("shutting down HTTP server")
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func dialOptions(cfg config) []grpc.DialOption {
	if cfg.tls {
		return []grpc.DialOption{
			grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		}
	}
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}
}
