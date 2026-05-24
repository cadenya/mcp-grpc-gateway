package discovery_test

import (
	"context"
	"net"
	"testing"

	"cadenya.com/mcp-grpc-gateway/internal/discovery"
	"cadenya.com/mcp-grpc-gateway/internal/testpb"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
)

type DiscoverySuite struct {
	suite.Suite
}

func TestDiscoverySuite(t *testing.T) {
	suite.Run(t, new(DiscoverySuite))
}

func (s *DiscoverySuite) TestFindsServiceInDescriptorSet() {
	fd := &descriptorpb.FileDescriptorProto{
		Name:    ptr("test/v1/echo.proto"),
		Package: ptr("test.v1"),
		Syntax:  ptr("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: ptr("EchoRequest")},
			{Name: ptr("EchoResponse")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: ptr("EchoService"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       ptr("Echo"),
				InputType:  ptr(".test.v1.EchoRequest"),
				OutputType: ptr(".test.v1.EchoResponse"),
			}},
		}},
	}
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fd}})
	s.Require().NoError(err)

	got, err := discovery.FindService(files, "test.v1.EchoService")

	s.Require().NoError(err)
	s.Equal("EchoService", string(got.Name()))
}

func (s *DiscoverySuite) TestReturnsUsefulErrorWhenServiceIsMissing() {
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{})
	s.Require().NoError(err)

	_, err = discovery.FindService(files, "test.v1.Missing")

	s.Require().Error(err)
	s.Contains(err.Error(), "service test.v1.Missing")
}

func (s *DiscoverySuite) TestLoadsFilesForSymbolFromGRPCReflection() {
	addr, stop := startGRPCServer(s.T())
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)
	defer conn.Close()

	files, err := discovery.LoadFilesForSymbol(context.Background(), conn, "functional.v1.GreeterService")

	s.Require().NoError(err)
	desc, err := files.FindDescriptorByName("functional.v1.GreeterService")
	s.Require().NoError(err)
	s.Equal("GreeterService", string(desc.Name()))
}

func startGRPCServer(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	server := grpc.NewServer()
	testpb.RegisterGreeterServiceServer(server, greeterServer{})
	reflection.Register(server)
	go func() {
		_ = server.Serve(listener)
	}()

	return listener.Addr().String(), func() {
		server.Stop()
		_ = listener.Close()
	}
}

type greeterServer struct {
	testpb.UnimplementedGreeterServiceServer
}

func (greeterServer) Greet(_ context.Context, req *testpb.GreetRequest) (*testpb.GreetResponse, error) {
	return &testpb.GreetResponse{Greeting: "Hello, " + req.GetName()}, nil
}

func ptr[T any](v T) *T {
	return &v
}
