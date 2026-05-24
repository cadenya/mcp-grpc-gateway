package toolcache

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"cadenya.com/mcp-grpc-gateway/internal/discovery"
	"cadenya.com/mcp-grpc-gateway/internal/gateway"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Loader func(context.Context, grpc.ClientConnInterface, string) (protoreflect.ServiceDescriptor, error)

type Options struct {
	Conn                   grpc.ClientConnInterface
	Service                string
	Loader                 Loader
	Logger                 *slog.Logger
	Tracer                 trace.Tracer
	RequireToolAnnotations bool
}

type Cache struct {
	conn    grpc.ClientConnInterface
	service string

	mu                     sync.RWMutex
	loader                 Loader
	logger                 *slog.Logger
	tracer                 trace.Tracer
	requireToolAnnotations bool
	current                atomic.Pointer[mcp.Server]
	version                atomic.Uint64
}

func New(opts Options) *Cache {
	loader := opts.Loader
	if loader == nil {
		loader = discovery.LoadService
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	tracer := opts.Tracer
	if tracer == nil {
		tracer = otel.Tracer("cadenya.com/mcp-grpc-gateway/internal/toolcache")
	}
	return &Cache{
		conn:                   opts.Conn,
		service:                opts.Service,
		loader:                 loader,
		logger:                 logger,
		tracer:                 tracer,
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
	ctx, span := c.tracer.Start(ctx, "toolcache.reload", trace.WithAttributes(attribute.String("grpc.service", c.service)))
	defer span.End()

	c.mu.RLock()
	loader := c.loader
	c.mu.RUnlock()
	if loader == nil {
		err := fmt.Errorf("tool cache loader is nil")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.logger.Error("reload reflected tools failed", "grpc_service", c.service, "error", err)
		return err
	}

	service, err := loader(ctx, c.conn, c.service)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.logger.Error("reload reflected tools failed", "grpc_service", c.service, "error", err)
		return err
	}
	server := gateway.NewServer(service)
	registerOpts := []gateway.RegisterOption{}
	if c.requireToolAnnotations {
		registerOpts = append(registerOpts, gateway.WithRequireToolAnnotations(true))
	}
	if err := gateway.RegisterTools(server, c.conn, service, registerOpts...); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.logger.Error("register reflected tools failed", "grpc_service", c.service, "error", err)
		return err
	}
	c.current.Store(server)
	version := c.version.Add(1)
	span.SetAttributes(attribute.Int64("toolcache.version", int64(version)))
	c.logger.Info("reloaded reflected tools", "grpc_service", c.service, "version", version)
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
