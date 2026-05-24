# Docker Compose Example

This example runs a small reflected gRPC greeter service beside the published MCP gRPC Gateway Docker image.

The greeter uses the annotated `functional.v1.GreeterService` proto from this repository. The gateway discovers that service through gRPC reflection and exposes the annotated `greet_user` RPC as an MCP tool.

## Run

From this directory:

```bash
docker compose up --build
```

The compose stack exposes:

- MCP HTTP endpoint: `http://localhost:8080/mcp`

The gRPC greeter listens on `greeter:50051` inside the compose network. It is not published to the host because the gateway is the public entry point for this example.

## Initialize MCP

```bash
curl -sS http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"demo"}}}'
```

## List Tools

```bash
curl -sS http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```

You should see a `greet_user` tool.

## Call The Tool

```bash
curl -sS http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'Authorization: Bearer demo-token' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"greet_user","arguments":{"name":"Ada"}}}'
```

You should see a structured response like:

```json
{"greeting":"Hello, Ada"}
```

The greeter container logs should also show that `Authorization` metadata was present. The example intentionally does not log the header value.

```bash
docker compose logs greeter
```

## Stop

```bash
docker compose down
```
