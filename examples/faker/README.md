# Faker MCP Example

This example runs a reflected gRPC service backed by `github.com/jaswdr/faker/v2` and exposes it through MCP gRPC Gateway.

It models an endpoint like:

```text
https://free.cadenya.com/mcp/faker
```

The local compose stack exposes the same MCP path at:

```text
http://localhost:8080/mcp/faker
```

## Tools

The gRPC service defines two MCP tools:

- `GetFakerOptions` lists supported fake data generators. Pass `filter` to match by name, category, description, or argument metadata.
- `GenerateFake` generates one fake value for a given option name and optional arguments.

The service builds its catalog from the exported faker-go API at startup. It includes every reflected faker method that can be called safely with JSON-friendly scalar arguments and returned as text or JSON. File/image generators are intentionally skipped because they create local temp files rather than MCP-friendly data values.

Example faker names include `person.name`, `internet.email`, `company.name`, `address.city`, `lorem.sentence`, `faker.int_between`, `uuid.v4`, and `color.hex`.

## Run

From this directory:

```bash
docker compose up --build
```

## Initialize MCP

```bash
curl -sS http://localhost:8080/mcp/faker \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"demo"}}}'
```

## List Tools

```bash
curl -sS http://localhost:8080/mcp/faker \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```

You should see `GetFakerOptions` and `GenerateFake`.

## List Faker Options

```bash
curl -sS http://localhost:8080/mcp/faker \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"GetFakerOptions","arguments":{"filter":"email"}}}'
```

## Generate Fake Data

```bash
curl -sS http://localhost:8080/mcp/faker \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"GenerateFake","arguments":{"name":"internet.email"}}}'
```

Some generators accept arguments. `GetFakerOptions` returns argument names, types, descriptions, and defaults.

```bash
curl -sS http://localhost:8080/mcp/faker \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"GenerateFake","arguments":{"name":"lorem.sentence","args":{"count":12}}}}'
```

## Stop

```bash
docker compose down
```
