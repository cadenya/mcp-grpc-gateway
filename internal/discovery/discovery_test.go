package discovery_test

import (
	"testing"

	"cadenya.com/mcp-grpc-gateway/internal/discovery"
	"github.com/stretchr/testify/suite"
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

func ptr[T any](v T) *T {
	return &v
}
