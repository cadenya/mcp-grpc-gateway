# Kubernetes Example

This example runs a small reflected gRPC greeter service beside the MCP gRPC Gateway in Kubernetes.

The greeter uses the annotated `functional.v1.GreeterService` proto from this repository. The gateway discovers services through gRPC reflection, loads all non-reflection services by default, and exposes the annotated `greet_user` RPC as an MCP tool.

## Build Images

For Docker Desktop Kubernetes, build both images into the local Docker image store:

```bash
docker build -t cadenyaagents/mcp-grpc-gateway:latest ../..
docker build -t mcp-grpc-gateway-greeter:latest -f ../docker-compose/greeter/Dockerfile ../..
```

## Deploy

From this directory:

```bash
kubectl apply -f greeter.yaml
kubectl apply -f gateway.yaml
kubectl rollout status deployment/greeter
kubectl rollout status deployment/mcp-grpc-gateway
```

The greeter is available inside the cluster at `greeter:50051`. The gateway is available inside the cluster at `mcp-grpc-gateway:8080`.

## Port Forward

```bash
kubectl port-forward svc/mcp-grpc-gateway 8080:8080
```

The MCP HTTP endpoint is then available at `http://localhost:8080/mcp`.

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

The greeter logs should also show that the `Authorization` header was forwarded to gRPC metadata:

```bash
kubectl logs deployment/greeter
```

## Clean Up

```bash
kubectl delete -f gateway.yaml
kubectl delete -f greeter.yaml
```
