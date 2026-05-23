# MCP gRPC Gateway

MCP is a protocol that LLM toolchains natively support. gRPC is an excellent RPC framework that is supported by the most popular languages. Together, they can pair well describe services with the server framework in gRPC with MCP as the tool discovery/execution protocol.

This project was designed to act as a gateway for MCP to your gRPC services. It uses protobuf annotations to describe tool names and descriptions for RPC descritors, as well as gRPC reflection to discover tools. This pattern means that this gateway doesn't need to be redployed for new tools to be discovered from your gRPC services.

## Quick Start

To run this as a binary, you can run:

```bash
mcp-grpc-gateway --addr 0.0.0.0:8080 --grpc-host your-grpc-service:50051 --service "youapp.v1.Service" --path "/mcp"
```

This will start a server that listens on port 8080, and will read the RPC definitions at `your-grpc-service:50051` and host your MCP endpoint at `/mcp`.

## Annotations

By default your RPC definitions in your gRPC endpoint will be exposed 1:1 for RPC names as tools. You can override this behavior (and add tool descriptions for LLMs to use) with annotations.

```proto
syntax = "proto3";

package yourapp.v1;

import "buf.build/cadenya-agents/mcp-grpc-gateway"

service Service {
  rpc GetRecentObjectives(RecentObjectivesRequest) returns (ObjectiveObjectivesResponse) {
    option (grpcmcpgateway.v1.Tool) = {
      name: "RecentObjectives"
      description: "Retrieves all of the recent objectives for the workspace that is authenticated"
    };
  }
}
```
