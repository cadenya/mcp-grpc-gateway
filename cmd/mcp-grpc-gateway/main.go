package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"cadenya.com/mcp-grpc-gateway/internal/toolcache"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type config struct {
	addr           string
	grpcHost       string
	service        string
	path           string
	tls            bool
	reloadInterval time.Duration
}

func main() {
	if err := newCommand(run).Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
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
		addr:           cmd.String("addr"),
		grpcHost:       cmd.String("grpc-host"),
		service:        cmd.String("service"),
		path:           cmd.String("path"),
		tls:            cmd.Bool("tls"),
		reloadInterval: cmd.Duration("reload-interval"),
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
	conn, err := grpc.NewClient(cfg.grpcHost, dialOptions(cfg)...)
	if err != nil {
		return fmt.Errorf("create grpc client: %w", err)
	}
	defer conn.Close()

	cache := toolcache.New(toolcache.Options{
		Conn:    conn,
		Service: cfg.service,
	})
	if err := cache.Reload(ctx); err != nil {
		return err
	}
	if cfg.reloadInterval > 0 {
		go cache.Run(ctx, cfg.reloadInterval)
	}

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return cache.Current()
	}, &mcp.StreamableHTTPOptions{})
	mux := http.NewServeMux()
	mux.Handle(cfg.path, handler)

	log.Printf("serving MCP endpoint on %s%s for gRPC service %s", cfg.addr, cfg.path, cfg.service)
	return http.ListenAndServe(cfg.addr, mux)
}

func dialOptions(cfg config) []grpc.DialOption {
	if cfg.tls {
		return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, ""))}
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
}
