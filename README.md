# MCP gRPC Gateway

<p align="center">
  <img src="images/diagram.png" alt="MCP client calling MCP gRPC Gateway, which forwards to your gRPC service" width="520">
</p>

MCP gRPC Gateway exposes existing gRPC services as stateless MCP tools over HTTP. It connects to a downstream gRPC server, discovers service descriptors through gRPC reflection or a proto descriptor file, converts unary RPC request messages into JSON Schema tool inputs, and invokes the selected RPC when an MCP client calls the tool.

The gateway is designed for teams that already describe service contracts in protobuf and want MCP support without hand-writing a parallel tool server. Protobuf annotations can provide tool names and tool descriptions, while reflection or descriptor files let the gateway reload tool definitions as services change. In practice, your gRPC service remains the source of truth and the gateway can pick up newly deployed tools without a gateway redeploy.

## Run The Gateway

MCP gRPC Gateway runs as a small HTTP service in front of a gRPC server. Point it at a gRPC host that has server reflection enabled:

```bash
mcp-grpc-gateway --grpc-host your-grpc-service:50051
```

By default, the gateway listens on `127.0.0.1:8080` and exposes the MCP endpoint at `/mcp`.

```text
http://127.0.0.1:8080/mcp
```

To expose it from a container, VM, or Kubernetes pod, bind to all interfaces explicitly:

```bash
mcp-grpc-gateway \
  --addr 0.0.0.0:8080 \
  --grpc-host your-grpc-service:50051
```

You can limit which reflected services become MCP tools with `--service`, and you can require explicit protobuf tool annotations with `--require-tool-annotations`:

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --service yourapp.v1.ObjectivesService \
  --require-tool-annotations
```

## Install

Install the gateway from source with Go:

```bash
go install go.cadenya.com/mcp-grpc-gateway/cmd/mcp-grpc-gateway@latest
```

For local development from a checked-out repository:

```bash
go install ./cmd/mcp-grpc-gateway
```

## Docker

Run the published Docker image:

```bash
docker run --rm -p 8080:8080 cadenyaagents/mcp-grpc-gateway:latest \
  --addr 0.0.0.0:8080 \
  --grpc-host your-grpc-service:50051
```

The runtime image has no shell or package manager and runs as a non-root user.

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

## MCP Transport

This gateway only supports stateless MCP over HTTP. It mounts the MCP Go SDK's Streamable HTTP transport with stateless JSON responses, so each request is handled independently and the gateway does not issue or require `Mcp-Session-Id` headers.

The endpoint is intended for HTTP `POST` requests with JSON responses. **It does not expose stdio, stateful SSE sessions, resumable streams, or event-store backed session recovery.**

## Forwarding Headers

HTTP headers are not forwarded to gRPC by default. You can opt in to specific headers with `--forward-header`; each matching HTTP header is attached to the downstream gRPC request as metadata.

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --service "yourapp.v1.Service" \
  --forward-header Authorization
```

Repeat the flag to allow more headers:

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --service "yourapp.v1.Service" \
  --forward-header Authorization \
  --forward-header X-Request-ID
```

## Service Filters

By default the gateway loads all non-reflection services exposed by the downstream gRPC server's reflection API. This is useful for production servers that host multiple gRPC services on the same listener.

Use `--service` when you want to expose only specific services. The flag may be repeated, and tools from the selected services are appended into one MCP server.

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --service "yourapp.v1.ObjectivesService" \
  --service "yourapp.v1.UsersService"
```

Tool names must be unique across all loaded services. If two RPCs produce the same MCP tool name, the first one is kept and the gateway emits a warning log with the colliding service name and tool name.

## gRPC Client TLS

The gateway connects to downstream gRPC services without TLS by default. Enable TLS with `--grpc-tls`; the existing `--tls` flag is kept as an alias.

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --grpc-tls
```

By default TLS uses the system root CAs. Provide a custom CA bundle when your gRPC service uses a private CA:

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --grpc-tls \
  --grpc-ca-file /etc/certs/ca.pem
```

For mTLS, provide both the client certificate and private key:

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --grpc-tls \
  --grpc-ca-file /etc/certs/ca.pem \
  --grpc-client-cert-file /etc/certs/client.crt \
  --grpc-client-key-file /etc/certs/client.key
```

Use `--grpc-server-name` when the certificate DNS name differs from `--grpc-host`.

```bash
mcp-grpc-gateway \
  --grpc-host 10.0.0.12:50051 \
  --grpc-tls \
  --grpc-server-name api.internal.example.com
```

Each TLS flag also has an environment variable: `GRPC_TLS`, `GRPC_CA_FILE`, `GRPC_CLIENT_CERT_FILE`, `GRPC_CLIENT_KEY_FILE`, and `GRPC_SERVER_NAME`.

## MCP Server Metadata

MCP server metadata belongs to the gateway process, not the reflected gRPC services. A single MCP server can aggregate tools from multiple gRPC services, so service-level protobuf annotations are not used to set the MCP server name, title, version, instructions, or website URL.

Configure those values with CLI flags:

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --mcp-name "objectives-gateway" \
  --mcp-title "Objectives Gateway" \
  --mcp-version "1.0.0" \
  --mcp-instructions "Use these tools to inspect workspace objectives." \
  --mcp-website-url "https://yourapp.example.com"
```

The same values can be set with environment variables:

```bash
MCP_NAME=objectives-gateway
MCP_TITLE="Objectives Gateway"
MCP_VERSION=1.0.0
MCP_INSTRUCTIONS="Use these tools to inspect workspace objectives."
MCP_WEBSITE_URL=https://yourapp.example.com
```

CLI flags override environment variables. By default, the gateway reports `mcp-grpc-gateway` as the MCP server name and `dev` as the version.

## Annotations

By default your RPC definitions in your gRPC endpoint will be exposed 1:1 for RPC names as tools. You can override tool names and add tool descriptions for LLMs with method annotations.

```proto
syntax = "proto3";

package yourapp.v1;

import "grpcmcpgateway/v1/annotations.proto";

service Service {
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
mcp-grpc-gateway --grpc-host your-grpc-service:50051 --require-tool-annotations
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

4. Add MCP tool annotations.

```proto
service ObjectivesService {
  option (grpcmcpgateway.v1.service) = {
    tool_prefix: "objectives_"
  };

  rpc ListObjectives(ListObjectivesRequest) returns (ListObjectivesResponse) {
    option (grpcmcpgateway.v1.tool) = {
      name: "list"
      description: "Lists objectives for the current workspace."
    };
  }
}
```

Service-level `tool_prefix` is prepended to every tool name in that service. In the example above, the MCP tool is exposed as `objectives_list`. This is useful when one gateway aggregates multiple services that might otherwise use the same tool names.

## Proto Descriptor Files

When gRPC reflection is not available on the downstream server, you can provide a binary protobuf `FileDescriptorSet` instead. This is the same format Envoy uses for gRPC-JSON transcoding.

Generate the descriptor set with Buf:

```bash
buf build -o descriptors.binpb
```

Then start the gateway with `--proto-descriptor`:

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --proto-descriptor descriptors.binpb
```

When `--proto-descriptor` is set, the gateway reads service definitions from the file and does not use gRPC reflection. The `--service` filter still works to limit which services are exposed:

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --proto-descriptor descriptors.binpb \
  --service yourapp.v1.ObjectivesService
```

Background reloads still apply — the gateway re-reads the descriptor file on each reload interval, so you can update the file in place and the gateway picks up the changes without a restart.

The flag can also be set with the `PROTO_DESCRIPTOR` environment variable.

## Tool Snapshot Reloads

The gateway keeps a reflected snapshot of your gRPC services and periodically reloads it. When a reload succeeds, new MCP sessions use the new tool set. When reflection fails during a rolling deploy, the gateway keeps serving the last known-good snapshot.

By default snapshots reload every minute:

```bash
mcp-grpc-gateway --grpc-host your-grpc-service:50051 --reload-interval 1m
```

You can disable background reloads by setting the interval to `0`:

```bash
mcp-grpc-gateway --grpc-host your-grpc-service:50051 --reload-interval 0
```

## Tool Call Timeouts

Downstream gRPC tool calls have a 30 second deadline by default. Configure it with `--tool-call-timeout` or `TOOL_CALL_TIMEOUT`.

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --tool-call-timeout 10s
```

Set the timeout to `0` to disable the gateway-level deadline.

## Logging

The gateway logs with Go's structured `slog` package. Logs are written to stderr, with `text` output by default.

```bash
mcp-grpc-gateway --grpc-host your-grpc-service:50051 --log-level debug --log-format json
```

Supported log levels are `debug`, `info`, `warn`, and `error`. Supported formats are `text` and `json`.

## OpenTelemetry

Tracing is disabled unless an OTLP gRPC endpoint is configured. When enabled, the gateway emits spans for tool snapshot reloads, MCP HTTP requests, and downstream gRPC tool calls.

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --otel-endpoint collector:4317
```

For local collectors that do not use TLS, add `--otel-insecure`:

```bash
mcp-grpc-gateway \
  --grpc-host your-grpc-service:50051 \
  --otel-endpoint localhost:4317 \
  --otel-insecure
```

The gateway uses W3C Trace Context and Baggage propagation. Incoming MCP HTTP request headers such as `traceparent`, `tracestate`, and `baggage` are extracted, and trace context is injected into downstream gRPC metadata for tool calls.
