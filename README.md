# MCP gRPC Gateway

MCP is a protocol that LLM toolchains natively support. gRPC is an excellent RPC framework that is supported by the most popular languages. Together, they can pair well to describe services with the server framework in gRPC with MCP as the tool discovery/execution protocol.

This project was designed to act as a gateway for MCP to your gRPC services. It uses protobuf annotations to describe MCP server and tool metadata, as well as gRPC reflection to discover tools. This pattern means that this gateway doesn't need to be redeployed for new tools to be discovered from your gRPC services.

## Quick Start

To run this as a binary, you can run:

```bash
mcp-grpc-gateway --addr 0.0.0.0:8080 --grpc-host your-grpc-service:50051 --service "youapp.v1.Service" --path "/mcp"
```

This will start a server that listens on port 8080, reads the RPC definitions at `your-grpc-service:50051`, and hosts your MCP endpoint at `/mcp`.

## Docker

The project publishes a distroless, non-root Docker image to Docker Hub:

```bash
docker run --rm -p 8080:8080 cadenyaagents/mcp-grpc-gateway:latest \
  --grpc-host your-grpc-service:50051 \
  --service "yourapp.v1.Service"
```

The runtime image has no shell or package manager and runs as a non-root user. GitHub Actions builds the image for `linux/amd64` and `linux/arm64`, and pushes to `cadenyaagents/mcp-grpc-gateway` on `main` when `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` repository secrets are configured.

## Releases

GitHub releases are driven by tags. Pushing a `v*` tag runs GoReleaser, publishes release archives and checksums, pushes Docker release tags, and labels the Buf module with the same tag.

```bash
git tag v0.1.0
git push origin v0.1.0
```

For `v0.1.0`, the release workflow publishes:

```text
GitHub release: v0.1.0
Docker tags: cadenyaagents/mcp-grpc-gateway:0.1.0, :0.1, :0, :latest, :sha-<commit>
Buf label: buf.build/cadenya-agents/mcp-grpc-gateway:v0.1.0
```

The release workflow requires `BUF_TOKEN`, `DOCKERHUB_USERNAME`, and `DOCKERHUB_TOKEN` repository secrets.

## MCP Transport

This gateway only supports stateless MCP over HTTP. It mounts the MCP Go SDK's Streamable HTTP transport with stateless JSON responses, so each request is handled independently and the gateway does not issue or require `Mcp-Session-Id` headers.

The endpoint is intended for HTTP `POST` requests with JSON responses. It does not expose stdio, stateful SSE sessions, resumable streams, or event-store backed session recovery.

## Annotations

By default your RPC definitions in your gRPC endpoint will be exposed 1:1 for RPC names as tools. You can override this behavior, add tool descriptions for LLMs, and configure MCP server metadata with annotations.

```proto
syntax = "proto3";

package yourapp.v1;

import "grpcmcpgateway/v1/annotations.proto";

service Service {
  option (grpcmcpgateway.v1.server) = {
    name: "objectives"
    title: "Objectives"
    version: "1.0.0"
    instructions: "Use these tools to inspect recent workspace objectives."
    website_url: "https://yourapp.example.com"
  };

  rpc GetRecentObjectives(RecentObjectivesRequest) returns (ObjectiveObjectivesResponse) {
    option (grpcmcpgateway.v1.tool) = {
      name: "recent_objectives"
      description: "Retrieves all of the recent objectives for the workspace that is authenticated"
    };
  }
}
```

If you want developers to explicitly disclose which RPCs become MCP tools, start the gateway with `--require-tool-annotations`. In that mode, only unary RPCs with `grpcmcpgateway.v1.tool` annotations are exposed.

```bash
mcp-grpc-gateway --grpc-host your-grpc-service:50051 --service "yourapp.v1.Service" --require-tool-annotations
```

## Publishing Protos

The annotations live in this repository under `proto/grpcmcpgateway/v1/annotations.proto`. The Buf module is named in `buf.yaml` so it can be published to the Buf Schema Registry.

To publish from a developer machine:

```bash
buf registry login
buf push
```

The GitHub Actions workflow also runs `buf push` on pushes to `main`. To enable it, create a Buf API token and add it to this GitHub repository as an Actions secret named `BUF_TOKEN`.

Consumers can import the annotations from the BSR module declared in `buf.yaml`:

```yaml
version: v2
deps:
  - buf.build/cadenya-agents/mcp-grpc-gateway
```

## Buf Examples

To use the annotations from another Buf-managed gRPC service:

1. Add the dependency to your `buf.yaml`.

```yaml
version: v2
deps:
  - buf.build/cadenya-agents/mcp-grpc-gateway
```

2. Update your Buf dependencies.

```bash
buf dep update
```

3. Import the annotations in your service proto.

```proto
import "grpcmcpgateway/v1/annotations.proto";
```

4. Add MCP server and tool annotations.

```proto
service ObjectivesService {
  option (grpcmcpgateway.v1.server) = {
    name: "objectives"
    title: "Objectives"
    version: "1.0.0"
  };

  rpc ListObjectives(ListObjectivesRequest) returns (ListObjectivesResponse) {
    option (grpcmcpgateway.v1.tool) = {
      name: "list_objectives"
      description: "Lists objectives for the current workspace."
    };
  }
}
```

## Tool Snapshot Reloads

The gateway keeps a reflected snapshot of your gRPC service and periodically reloads it. When a reload succeeds, new MCP sessions use the new tool set. When reflection fails during a rolling deploy, the gateway keeps serving the last known-good snapshot.

By default snapshots reload every minute:

```bash
mcp-grpc-gateway --grpc-host your-grpc-service:50051 --service "yourapp.v1.Service" --reload-interval 1m
```

You can disable background reloads by setting the interval to `0`:

```bash
mcp-grpc-gateway --grpc-host your-grpc-service:50051 --service "yourapp.v1.Service" --reload-interval 0
```

## Logging

The gateway logs with Go's structured `slog` package. Logs are written to stderr, with `text` output by default.

```bash
mcp-grpc-gateway --grpc-host your-grpc-service:50051 --service "yourapp.v1.Service" --log-level debug --log-format json
```

Supported log levels are `debug`, `info`, `warn`, and `error`. Supported formats are `text` and `json`.

## OpenTelemetry

Tracing is disabled unless an OTLP gRPC endpoint is configured. When enabled, the gateway emits spans for tool snapshot reloads and downstream gRPC tool calls.

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --service "yourapp.v1.Service" \
  --otel-endpoint collector:4317
```

For local collectors that do not use TLS, add `--otel-insecure`:

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --service "yourapp.v1.Service" \
  --otel-endpoint localhost:4317 \
  --otel-insecure
```
