package toolcache

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.cadenya.com/mcp-grpc-gateway/internal/discovery"
	"go.cadenya.com/mcp-grpc-gateway/internal/gateway"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Loader func(context.Context, grpc.ClientConnInterface, []string) ([]protoreflect.ServiceDescriptor, error)

type ServerMetadata = gateway.ServerMetadata

type Options struct {
	Conn                   grpc.ClientConnInterface
	Service                string
	Services               []string
	Server                 ServerMetadata
	Loader                 Loader
	Logger                 *slog.Logger
	Tracer                 trace.Tracer
	RequireToolAnnotations bool
}

type Cache struct {
	conn     grpc.ClientConnInterface
	services []string

	mu                     sync.RWMutex
	loader                 Loader
	logger                 *slog.Logger
	tracer                 trace.Tracer
	server                 ServerMetadata
	requireToolAnnotations bool
	current                atomic.Pointer[mcp.Server]
	version                atomic.Uint64
}

func New(opts Options) *Cache {
	loader := opts.Loader
	if loader == nil {
		loader = discovery.LoadServices
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	tracer := opts.Tracer
	if tracer == nil {
		tracer = otel.Tracer("go.cadenya.com/mcp-grpc-gateway/internal/toolcache")
	}
	services := opts.Services
	if len(services) == 0 && opts.Service != "" {
		services = []string{opts.Service}
	}
	return &Cache{
		conn:                   opts.Conn,
		services:               services,
		loader:                 loader,
		logger:                 logger,
		tracer:                 tracer,
		server:                 opts.Server,
		requireToolAnnotations: opts.RequireToolAnnotations,
	}
}

func (c *Cache) Current() *mcp.Server {
	return c.current.Load()
}

func (c *Cache) Version() uint64 {
	return c.version.Load()
}

func (c *Cache) SetLoader(loader Loader) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loader = loader
}

func (c *Cache) Reload(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "toolcache.reload", trace.WithAttributes(attribute.StringSlice("grpc.services", c.services)))
	defer span.End()

	c.mu.RLock()
	loader := c.loader
	c.mu.RUnlock()
	if loader == nil {
		err := fmt.Errorf("tool cache loader is nil")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.logger.Error("reload reflected tools failed", "grpc_services", c.services, "error", err)
		return err
	}

	services, err := loader(ctx, c.conn, c.services)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.logger.Error("reload reflected tools failed", "grpc_services", c.services, "error", err)
		return err
	}
	if len(services) == 0 {
		err := fmt.Errorf("no gRPC services discovered")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.logger.Error("reload reflected tools failed", "grpc_services", c.services, "error", err)
		return err
	}
	server := gateway.NewServer(c.server)
	registerOpts := []gateway.RegisterOption{}
	if c.requireToolAnnotations {
		registerOpts = append(registerOpts, gateway.WithRequireToolAnnotations(true))
	}
	registered := map[string]string{}
	registerOpts = append(registerOpts, gateway.WithRegisteredToolNames(registered), gateway.WithLogger(c.logger))
	for _, service := range services {
		if err := gateway.RegisterTools(server, c.conn, service, registerOpts...); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			c.logger.Error("register reflected tools failed", "grpc_service", string(service.FullName()), "error", err)
			return err
		}
	}
	c.current.Store(server)
	version := c.version.Add(1)
	span.SetAttributes(attribute.Int64("toolcache.version", int64(version)))
	c.logger.Info("reloaded reflected tools", "grpc_services", serviceNames(services), "version", version)
	return nil
}

func (c *Cache) Run(ctx context.Context, interval time.Duration) error {
	if err := c.Reload(ctx); err != nil {
		return err
	}
	if interval <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_ = c.Reload(ctx)
		}
	}
}

func serviceNames(services []protoreflect.ServiceDescriptor) []string {
	names := make([]string, 0, len(services))
	for _, service := range services {
		names = append(names, string(service.FullName()))
	}
	return names
}
