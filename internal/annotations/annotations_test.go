package annotations_test

import (
	"testing"

	"cadenya.com/mcp-grpc-gateway/internal/annotations"
	"github.com/stretchr/testify/suite"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type MetadataSuite struct {
	suite.Suite
	file protoreflect.FileDescriptor
}

func TestMetadataSuite(t *testing.T) {
	suite.Run(t, new(MetadataSuite))
}

func (s *MetadataSuite) SetupSuite() {
	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	typeString := descriptorpb.FieldDescriptorProto_TYPE_STRING
	typeMessage := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE

	fdProto := &descriptorpb.FileDescriptorProto{
		Name:    ptr("test/v1/test.proto"),
		Package: ptr("test.v1"),
		Syntax:  ptr("proto3"),
		Dependency: []string{
			"google/protobuf/descriptor.proto",
			"grpcmcpgateway/v1/annotations.proto",
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: ptr("Request")},
			{Name: ptr("Response")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: ptr("Service"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name:       ptr("Annotated"),
					InputType:  ptr(".test.v1.Request"),
					OutputType: ptr(".test.v1.Response"),
					Options:   &descriptorpb.MethodOptions{},
				},
				{
					Name:       ptr("Plain"),
					InputType:  ptr(".test.v1.Request"),
					OutputType: ptr(".test.v1.Response"),
				},
			},
		}},
	}

	baseFiles, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		protodesc.ToFileDescriptorProto(descriptorpb.File_google_protobuf_descriptor_proto),
	}})
	s.Require().NoError(err)

	toolFile, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    ptr("grpcmcpgateway/v1/annotations.proto"),
		Package: ptr("grpcmcpgateway.v1"),
		Syntax:  ptr("proto3"),
		Dependency: []string{
			"google/protobuf/descriptor.proto",
		},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: ptr("ToolOptions"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{Name: ptr("name"), JsonName: ptr("name"), Number: ptr[int32](1), Label: &optional, Type: &typeString},
				{Name: ptr("description"), JsonName: ptr("description"), Number: ptr[int32](2), Label: &optional, Type: &typeString},
			},
		}},
		Extension: []*descriptorpb.FieldDescriptorProto{{
			Name:     ptr("tool"),
			Number:   ptr[int32](51000),
			Label:    &optional,
			Type:     &typeMessage,
			TypeName: ptr(".grpcmcpgateway.v1.ToolOptions"),
			Extendee: ptr(".google.protobuf.MethodOptions"),
			JsonName: ptr("tool"),
		}},
	}, baseFiles)
	s.Require().NoError(err)

	ext := toolFile.Extensions().ByName("tool")
	tool := dynamicpb.NewMessage(ext.Message())
	tool.Set(ext.Message().Fields().ByName("name"), protoreflect.ValueOfString("RecentObjectives"))
	tool.Set(ext.Message().Fields().ByName("description"), protoreflect.ValueOfString("Retrieves recent objectives"))
	toolBytes, err := proto.Marshal(tool)
	s.Require().NoError(err)
	unknown := protowire.AppendTag(nil, protowire.Number(ext.Number()), protowire.BytesType)
	unknown = protowire.AppendBytes(unknown, toolBytes)
	fdProto.Service[0].Method[0].Options.ProtoReflect().SetUnknown(unknown)

	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		protodesc.ToFileDescriptorProto(descriptorpb.File_google_protobuf_descriptor_proto),
		protodesc.ToFileDescriptorProto(toolFile),
		fdProto,
	}})
	s.Require().NoError(err)
	fd, err := files.FindFileByPath("test/v1/test.proto")
	s.Require().NoError(err)
	s.file = fd
}

func (s *MetadataSuite) TestReadsToolAnnotationWhenPresent() {
	method := s.file.Services().ByName("Service").Methods().ByName("Annotated")

	got := annotations.ForMethod(method)

	s.Equal("RecentObjectives", got.Name)
	s.Equal("Retrieves recent objectives", got.Description)
}

func (s *MetadataSuite) TestFallsBackToRPCNameWhenAnnotationIsAbsent() {
	method := s.file.Services().ByName("Service").Methods().ByName("Plain")

	got := annotations.ForMethod(method)

	s.Equal("Plain", got.Name)
	s.Equal("Calls test.v1.Service/Plain", got.Description)
}

func ptr[T any](v T) *T {
	return &v
}
