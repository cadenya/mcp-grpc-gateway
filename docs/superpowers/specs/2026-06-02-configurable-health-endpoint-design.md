# Configurable Health Endpoint Design

## Goal

Add a configurable HTTP health endpoint to `mcp-grpc-gateway`.

The gateway should continue to expose health by default at `/health`, while allowing operators to choose another path with a CLI flag or environment variable.

## Configuration

- Add `healthPath string` to the command config.
- Add a CLI flag named `--health-path`.
- Add an environment variable source named `HEALTH_PATH`.
- Default the health path to `/health`.
- Normalize the value the same way as the existing MCP `--path` flag:
  - an empty value falls back to `/health`
  - a value without a leading slash gets one

## HTTP Behavior

The main HTTP mux should register:

- the MCP handler at `cfg.path`
- the health handler at `cfg.healthPath`

The health handler should return:

- status `200 OK`
- body `ok`

The health response should not depend on downstream gRPC discovery, tool calls, or MCP request handling.

## Error Handling

Reject configurations where the normalized MCP path and normalized health path are the same. Returning a clear validation error before mux registration avoids an `http.ServeMux` duplicate-handler panic for values such as `--path /health` or `--health-path /mcp`.

Other invalid or unusual path strings should follow `http.ServeMux` behavior, consistent with the existing MCP path handling.

## Tests

Add focused tests in `cmd/mcp-grpc-gateway/main_test.go` for:

- default `healthPath` is `/health`
- `--health-path` parses and normalizes values without a leading slash
- `HEALTH_PATH` populates the config
- the registered health endpoint returns `200 OK` and `ok`

Keep existing tests for MCP path behavior and graceful HTTP shutdown intact.

## Documentation

Update `README.md` to mention:

- default health endpoint: `/health`
- CLI flag: `--health-path`
- environment variable: `HEALTH_PATH`

Place the documentation near the basic run or transport information where operators look for exposed HTTP endpoints.
