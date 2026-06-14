package toolcache

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"go.cadenya.com/mcp-grpc-gateway/internal/discovery"
	"go.cadenya.com/mcp-grpc-gateway/internal/toolregistry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Loader func(context.Context, grpc.ClientConnInterface, []string) ([]protoreflect.ServiceDescriptor, error)

type ServerMetadata = toolregistry.ServerMetadata

type Options struct {
	Conn                   grpc.ClientConnInterface
	Service                string
	Services               []string
	Server                 ServerMetadata
	Loader                 Loader
	Logger                 *slog.Logger
	Tracer                 trace.Tracer
	RequireToolAnnotations bool
	ToolCallTimeout        time.Duration
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
	toolCallTimeout        time.Duration
	tools                  map[string]string
	current                atomic.Pointer[toolregistry.Registry]
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
		toolCallTimeout:        opts.ToolCallTimeout,
	}
}

func (c *Cache) Current() *toolregistry.Registry {
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
	registered := map[string]string{}
	registry, err := toolregistry.Build(toolregistry.BuildOptions{
		Conn:                    c.conn,
		Services:                services,
		Server:                  c.server,
		Logger:                  c.logger,
		RequireToolAnnotations:  c.requireToolAnnotations,
		RegisteredToolNameOwner: registered,
		ToolCallTimeout:         c.toolCallTimeout,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.logger.Error("register reflected tools failed", "grpc_services", serviceNames(services), "error", err)
		return err
	}
	c.current.Store(registry)
	version := c.version.Add(1)

	c.mu.Lock()
	added, removed, unchanged := diffTools(c.tools, registered)
	c.tools = registered
	c.mu.Unlock()

	span.SetAttributes(
		attribute.Int64("toolcache.version", int64(version)),
		attribute.Int("toolcache.tools.added", len(added)),
		attribute.Int("toolcache.tools.removed", len(removed)),
		attribute.Int("toolcache.tools.unchanged", len(unchanged)),
	)
	c.logger.Info("reloaded reflected tools",
		"grpc_services", serviceNames(services),
		"version", version,
		"tools_added", added,
		"tools_removed", removed,
		"tools_unchanged", len(unchanged),
	)
	return nil
}

// diffTools compares the previously registered tool set with the newly
// registered one, returning sorted slices of added and removed tool names and
// the names that were present in both reloads.
func diffTools(previous, current map[string]string) (added, removed, unchanged []string) {
	for name := range current {
		if _, ok := previous[name]; ok {
			unchanged = append(unchanged, name)
		} else {
			added = append(added, name)
		}
	}
	for name := range previous {
		if _, ok := current[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(unchanged)
	return added, removed, unchanged
}

func (c *Cache) Run(ctx context.Context, interval time.Duration) error {
	if err := c.Reload(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
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
