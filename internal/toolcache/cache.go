package toolcache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"cadenya.com/mcp-grpc-gateway/internal/discovery"
	"cadenya.com/mcp-grpc-gateway/internal/gateway"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Loader func(context.Context, grpc.ClientConnInterface, string) (protoreflect.ServiceDescriptor, error)

type Options struct {
	Conn    grpc.ClientConnInterface
	Service string
	Loader  Loader
}

type Cache struct {
	conn    grpc.ClientConnInterface
	service string

	mu      sync.RWMutex
	loader  Loader
	current atomic.Pointer[mcp.Server]
	version atomic.Uint64
}

func New(opts Options) *Cache {
	loader := opts.Loader
	if loader == nil {
		loader = discovery.LoadService
	}
	return &Cache{
		conn:    opts.Conn,
		service: opts.Service,
		loader:  loader,
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
	c.mu.RLock()
	loader := c.loader
	c.mu.RUnlock()
	if loader == nil {
		return fmt.Errorf("tool cache loader is nil")
	}

	service, err := loader(ctx, c.conn, c.service)
	if err != nil {
		return err
	}
	server := gateway.NewServer(service)
	if err := gateway.RegisterTools(server, c.conn, service); err != nil {
		return err
	}
	c.current.Store(server)
	c.version.Add(1)
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
