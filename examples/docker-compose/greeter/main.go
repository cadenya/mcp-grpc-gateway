package main

import (
	"context"
	"log"
	"net"

	"cadenya.com/mcp-grpc-gateway/internal/testpb"
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
	auth := firstMetadataValue(ctx, "authorization")
	if auth == "" {
		log.Printf("Greet name=%q authorization=<empty>", req.GetName())
	} else {
		log.Printf("Greet name=%q authorization=%q", req.GetName(), auth)
	}
	return &testpb.GreetResponse{Greeting: "Hello, " + req.GetName()}, nil
}

func firstMetadataValue(ctx context.Context, key string) string {
	values := metadata.ValueFromIncomingContext(ctx, key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
