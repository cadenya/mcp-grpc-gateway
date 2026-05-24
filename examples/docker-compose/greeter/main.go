package main

import (
	"context"
	"log"
	"net"

	"go.cadenya.com/mcp-grpc-gateway/internal/testpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
)

func main() {
	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	server := grpc.NewServer()
	testpb.RegisterGreeterServiceServer(server, greeterServer{})
	reflection.Register(server)

	log.Printf("greeter gRPC server listening on %s", listener.Addr())
	if err := server.Serve(listener); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

type greeterServer struct {
	testpb.UnimplementedGreeterServiceServer
}

func (greeterServer) Greet(ctx context.Context, req *testpb.GreetRequest) (*testpb.GreetResponse, error) {
	log.Printf("Greet name=%q authorization_present=%t", req.GetName(), hasMetadataValue(ctx, "authorization"))
	return &testpb.GreetResponse{Greeting: "Hello, " + req.GetName()}, nil
}

func hasMetadataValue(ctx context.Context, key string) bool {
	values := metadata.ValueFromIncomingContext(ctx, key)
	return len(values) > 0 && values[0] != ""
}
