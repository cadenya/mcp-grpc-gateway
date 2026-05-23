package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"cadenya.com/mcp-grpc-gateway/internal/discovery"
	"cadenya.com/mcp-grpc-gateway/internal/gateway"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	if err := run(context.Background(), cfg); err != nil {
		log.Fatal(err)
	}
}

func parseConfig(args []string) (config, error) {
	cfg := config{
		addr: "0.0.0.0:8080",
		path: "/mcp",
	}
	fs := flag.NewFlagSet("mcp-grpc-gateway", flag.ContinueOnError)
	fs.StringVar(&cfg.addr, "addr", cfg.addr, "HTTP listen address")
	fs.StringVar(&cfg.grpcHost, "grpc-host", "", "gRPC host to reflect and invoke")
	fs.StringVar(&cfg.service, "service", "", "fully-qualified gRPC service name")
	fs.StringVar(&cfg.path, "path", cfg.path, "HTTP path for the MCP endpoint")
	fs.BoolVar(&cfg.tls, "tls", false, "connect to gRPC using TLS with system roots")
	if err := fs.Parse(args); err != nil {
		return config{}, err
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
