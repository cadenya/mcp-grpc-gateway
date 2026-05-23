package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"cadenya.com/mcp-grpc-gateway/internal/discovery"
	"cadenya.com/mcp-grpc-gateway/internal/gateway"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type config struct {
	addr     string
	grpcHost string
	service  string
	path     string
	tls      bool
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
		addr:     cmd.String("addr"),
		grpcHost: cmd.String("grpc-host"),
		service:  cmd.String("service"),
		path:     cmd.String("path"),
		tls:      cmd.Bool("tls"),
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

	service, err := discovery.LoadService(ctx, conn, cfg.service)
	if err != nil {
		return err
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "mcp-grpc-gateway", Version: "dev"}, nil)
	if err := gateway.RegisterTools(server, conn, service); err != nil {
		return err
	}

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
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
