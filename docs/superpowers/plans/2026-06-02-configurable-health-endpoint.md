# Configurable Health Endpoint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a configurable health endpoint that defaults to `/health` and can be overridden with `--health-path` or `HEALTH_PATH`.

**Architecture:** Extend the existing command config and CLI flag parsing in `cmd/mcp-grpc-gateway/main.go`, then register a small health handler on the existing HTTP mux. Keep the health handler independent from MCP request handling and downstream gRPC tool invocation.

**Tech Stack:** Go, `github.com/urfave/cli/v3`, `net/http`, `github.com/stretchr/testify/require`.

---

### File Structure

- Modify `cmd/mcp-grpc-gateway/main.go`: add `healthPath` config, flag/env parsing, path normalization, and mux registration.
- Modify `cmd/mcp-grpc-gateway/main_test.go`: add command parsing tests and an HTTP handler test.
- Modify `README.md`: document default health path and configuration knobs.

### Task 1: Config And Health Handler

**Files:**
- Modify: `cmd/mcp-grpc-gateway/main.go`
- Modify: `cmd/mcp-grpc-gateway/main_test.go`

- [ ] **Step 1: Write failing tests for health path config and handler behavior**

Add these tests to `cmd/mcp-grpc-gateway/main_test.go` near the existing command parsing tests and HTTP server test:

```go
func TestCommandDefaultsHealthPath(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
	})

	require.NoError(t, err)
	require.Equal(t, "/health", got.healthPath)
}

func TestCommandParsesAndNormalizesHealthPath(t *testing.T) {
	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
		"--health-path", "readyz",
	})

	require.NoError(t, err)
	require.Equal(t, "/readyz", got.healthPath)
}

func TestCommandReadsHealthPathFromEnvironment(t *testing.T) {
	t.Setenv("HEALTH_PATH", "/livez")

	var got config
	cmd := newCommand(func(_ context.Context, cfg config) error {
		got = cfg
		return nil
	})

	err := cmd.Run(context.Background(), []string{
		"mcp-grpc-gateway",
		"--grpc-host", "localhost:50051",
	})

	require.NoError(t, err)
	require.Equal(t, "/livez", got.healthPath)
}

func TestHealthHandlerReturnsOK(t *testing.T) {
	mux := http.NewServeMux()
	registerHealthHandler(mux, "/readyz")

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	resp := httptest.NewRecorder()

	mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, "ok", resp.Body.String())
}
```

Also add this import if it is not already present:

```go
import "net/http/httptest"
```

- [ ] **Step 2: Run tests to verify they fail for missing health support**

Run:

```bash
go test ./cmd/mcp-grpc-gateway
```

Expected: FAIL because `config.healthPath` and `registerHealthHandler` do not exist, and possibly because `httptest` has not been imported yet.

- [ ] **Step 3: Add minimal config, flag, normalization, and handler registration**

In `cmd/mcp-grpc-gateway/main.go`, add `healthPath` to `config`:

```go
healthPath string
```

Add this flag near the existing `--path` flag:

```go
&cli.StringFlag{
	Name:    "health-path",
	Value:   "/health",
	Usage:   "HTTP path for the health endpoint",
	Sources: cli.EnvVars("HEALTH_PATH"),
},
```

In `configFromCommand`, populate and normalize the value:

```go
healthPath: cmd.String("health-path"),
```

```go
cfg.healthPath = normalizeHTTPPath(cfg.healthPath, "/health")
```

Replace the existing inline MCP path normalization with:

```go
cfg.path = normalizeHTTPPath(cfg.path, "/mcp")
```

Add this helper near `configFromCommand`:

```go
func normalizeHTTPPath(path, fallback string) string {
	if path == "" {
		path = fallback
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
```

Add this handler helper near `run`:

```go
func registerHealthHandler(mux *http.ServeMux, path string) {
	mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
}
```

Register it in `run` after the MCP handler is registered:

```go
registerHealthHandler(mux, cfg.healthPath)
```

Update the startup log to include the health path:

```go
logger.Info("serving MCP endpoint", "addr", listener.Addr().String(), "path", cfg.path, "health_path", cfg.healthPath, "grpc_services", cfg.services)
```

- [ ] **Step 4: Run command tests to verify they pass**

Run:

```bash
go test ./cmd/mcp-grpc-gateway
```

Expected: PASS.

- [ ] **Step 5: Commit code changes**

Run:

```bash
git add cmd/mcp-grpc-gateway/main.go cmd/mcp-grpc-gateway/main_test.go
git commit -m "feat: add configurable health endpoint"
```

### Task 2: README Documentation

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Write the documentation update**

In `README.md`, after the default MCP endpoint text in the "Run The Gateway" section, add:

```markdown
The gateway also exposes a health endpoint at `/health` by default:

```text
http://127.0.0.1:8080/health
```

Use `--health-path` or `HEALTH_PATH` to expose health at a different path:

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --health-path /readyz
```
```

- [ ] **Step 2: Run the full test suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Commit documentation**

Run:

```bash
git add README.md
git commit -m "docs: document configurable health endpoint"
```

### Task 3: Final Verification

**Files:**
- Verify: all changed files

- [ ] **Step 1: Check git status**

Run:

```bash
git status --short
```

Expected: only unrelated pre-existing files may remain untracked, such as `images/.DS_Store`.

- [ ] **Step 2: Run final test suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Summarize result**

Report changed behavior, tests run, and any unrelated worktree files left untouched.
