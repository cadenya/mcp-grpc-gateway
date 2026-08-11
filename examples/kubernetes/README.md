# Kubernetes Example

This example runs a small reflected gRPC greeter service beside the MCP gRPC Gateway in Kubernetes. It also runs a dummy OpenTelemetry Collector that receives OTLP gRPC traces from the gateway and prints them to its pod logs.

The greeter uses the annotated `functional.v1.GreeterService` proto from this repository. The gateway discovers services through gRPC reflection, loads all non-reflection services by default, and exposes the annotated `greet_user` RPC as an MCP tool.

## Build Images

For Docker Desktop Kubernetes, build both images into the local Docker image store:

```bash
docker build -t ghcr.io/cadenya/mcp-grpc-gateway:latest ../..
docker build -t mcp-grpc-gateway-greeter:latest -f ../docker-compose/greeter/Dockerfile ../..
```

## Deploy

From this directory:

```bash
kubectl apply -f greeter.yaml
kubectl apply -f otel-collector.yaml
kubectl apply -f gateway.yaml
kubectl rollout status deployment/greeter
kubectl rollout status deployment/otel-collector
kubectl rollout status deployment/mcp-grpc-gateway
```

The greeter is available inside the cluster at `greeter:50051`. The dummy OpenTelemetry endpoint is available inside the cluster at `otel-collector:4317`. The gateway is available inside the cluster at `mcp-grpc-gateway:8080`.

## Port Forward

```bash
kubectl port-forward svc/mcp-grpc-gateway 8080:8080
```

The MCP HTTP endpoint is then available at `http://localhost:8080/mcp`.

## Discover The MCP Server

```bash
curl -sS http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'Mcp-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: server/discover' \
  -d '{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"curl","version":"demo"},"io.modelcontextprotocol/clientCapabilities":{}}}}'
```

## List Tools

```bash
curl -sS http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'Mcp-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: tools/list' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"curl","version":"demo"},"io.modelcontextprotocol/clientCapabilities":{}}}}'
```

You should see a `greet_user` tool.

## Call The Tool

```bash
curl -sS http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'Mcp-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: tools/call' \
  -H 'Mcp-Name: greet_user' \
  -H 'Authorization: Bearer demo-token' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"curl","version":"demo"},"io.modelcontextprotocol/clientCapabilities":{}},"name":"greet_user","arguments":{"name":"Ada"}}}'
```

You should see a structured response like:

```json
{"greeting":"Hello, Ada"}
```

The greeter logs should also show that `Authorization` metadata was present. The example intentionally does not log the header value.

```bash
kubectl logs deployment/greeter
```

## Check Telemetry

The gateway is configured with `--otel-endpoint otel-collector:4317 --otel-insecure`. The collector uses the debug exporter, so received spans are printed to its logs.

```bash
kubectl logs deployment/otel-collector
```

After the gateway starts or reloads reflected tools, you should see trace output that includes a `toolcache.reload` span.

## Clean Up

```bash
kubectl delete -f gateway.yaml
kubectl delete -f otel-collector.yaml
kubectl delete -f greeter.yaml
```
