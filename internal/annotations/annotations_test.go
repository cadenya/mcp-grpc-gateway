package annotations_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	grpcmcpgatewayv1 "go.cadenya.com/mcp-grpc-gateway/gen/grpcmcpgateway/v1"
	"go.cadenya.com/mcp-grpc-gateway/internal/annotations"
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
			Name:    ptr("Service"),
			Options: &descriptorpb.ServiceOptions{},
			Method: []*descriptorpb.MethodDescriptorProto{
				{
					Name:       ptr("Annotated"),
					InputType:  ptr(".test.v1.Request"),
					OutputType: ptr(".test.v1.Response"),
					Options:    &descriptorpb.MethodOptions{},
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
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: ptr("Server"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: ptr("name"), JsonName: ptr("name"), Number: ptr[int32](1), Label: &optional, Type: &typeString},
					{Name: ptr("title"), JsonName: ptr("title"), Number: ptr[int32](2), Label: &optional, Type: &typeString},
					{Name: ptr("version"), JsonName: ptr("version"), Number: ptr[int32](3), Label: &optional, Type: &typeString},
					{Name: ptr("instructions"), JsonName: ptr("instructions"), Number: ptr[int32](4), Label: &optional, Type: &typeString},
					{Name: ptr("website_url"), JsonName: ptr("websiteUrl"), Number: ptr[int32](5), Label: &optional, Type: &typeString},
				},
			},
			{
				Name: ptr("Service"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: ptr("tool_prefix"), JsonName: ptr("toolPrefix"), Number: ptr[int32](1), Label: &optional, Type: &typeString},
				},
			},
			{
				Name: ptr("Tool"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: ptr("name"), JsonName: ptr("name"), Number: ptr[int32](1), Label: &optional, Type: &typeString},
					{Name: ptr("description"), JsonName: ptr("description"), Number: ptr[int32](2), Label: &optional, Type: &typeString},
				},
			},
		},
		Extension: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     ptr("server"),
				Number:   ptr(grpcmcpgatewayv1.ServerExtensionNumber),
				Label:    &optional,
				Type:     &typeMessage,
				TypeName: ptr(".grpcmcpgateway.v1.Server"),
				Extendee: ptr(".google.protobuf.ServiceOptions"),
				JsonName: ptr("server"),
			},
			{
				Name:     ptr("service"),
				Number:   ptr(grpcmcpgatewayv1.ServiceExtensionNumber),
				Label:    &optional,
				Type:     &typeMessage,
				TypeName: ptr(".grpcmcpgateway.v1.Service"),
				Extendee: ptr(".google.protobuf.ServiceOptions"),
				JsonName: ptr("service"),
			},
			{
				Name:     ptr("tool"),
				Number:   ptr(grpcmcpgatewayv1.ToolExtensionNumber),
				Label:    &optional,
				Type:     &typeMessage,
				TypeName: ptr(".grpcmcpgateway.v1.Tool"),
				Extendee: ptr(".google.protobuf.MethodOptions"),
				JsonName: ptr("tool"),
			},
		},
	}, baseFiles)
	s.Require().NoError(err)

	serverExt := toolFile.Extensions().ByName("server")
	server := dynamicpb.NewMessage(serverExt.Message())
	server.Set(serverExt.Message().Fields().ByName("name"), protoreflect.ValueOfString("objectives"))
	server.Set(serverExt.Message().Fields().ByName("title"), protoreflect.ValueOfString("Objectives"))
	server.Set(serverExt.Message().Fields().ByName("version"), protoreflect.ValueOfString("1.2.3"))
	server.Set(serverExt.Message().Fields().ByName("instructions"), protoreflect.ValueOfString("Use these tools to inspect objectives."))
	server.Set(serverExt.Message().Fields().ByName("website_url"), protoreflect.ValueOfString("https://example.com/objectives"))
	serverBytes, err := proto.Marshal(server)
	s.Require().NoError(err)
	serverUnknown := protowire.AppendTag(nil, protowire.Number(serverExt.Number()), protowire.BytesType)
	serverUnknown = protowire.AppendBytes(serverUnknown, serverBytes)
	serviceExt := toolFile.Extensions().ByName("service")
	serviceOptions := dynamicpb.NewMessage(serviceExt.Message())
	serviceOptions.Set(serviceExt.Message().Fields().ByName("tool_prefix"), protoreflect.ValueOfString("objectives_"))
	serviceBytes, err := proto.Marshal(serviceOptions)
	s.Require().NoError(err)
	serviceUnknown := protowire.AppendTag(serverUnknown, protowire.Number(serviceExt.Number()), protowire.BytesType)
	serviceUnknown = protowire.AppendBytes(serviceUnknown, serviceBytes)
	fdProto.Service[0].Options.ProtoReflect().SetUnknown(serviceUnknown)

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

func (s *MetadataSuite) TestReadsServerAnnotationWhenPresent() {
	service := s.file.Services().ByName("Service")

	got := annotations.ForService(service)

	s.Equal("objectives", got.Name)
	s.Equal("Objectives", got.Title)
	s.Equal("1.2.3", got.Version)
	s.Equal("Use these tools to inspect objectives.", got.Instructions)
	s.Equal("https://example.com/objectives", got.WebsiteURL)
}

func (s *MetadataSuite) TestReadsServiceToolPrefixWhenPresent() {
	service := s.file.Services().ByName("Service")

	got := annotations.ForService(service)

	s.Equal("objectives_", got.ToolPrefix)
}

func ptr[T any](v T) *T {
	return &v
}
