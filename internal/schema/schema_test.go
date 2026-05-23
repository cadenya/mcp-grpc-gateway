package schema_test

import (
	"testing"

	gatewayschema "cadenya.com/mcp-grpc-gateway/internal/schema"
	"github.com/stretchr/testify/suite"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

type ConverterSuite struct {
	suite.Suite
	file protoreflect.FileDescriptor
}

func TestConverterSuite(t *testing.T) {
	suite.Run(t, new(ConverterSuite))
}

func (s *ConverterSuite) SetupSuite() {
	required := descriptorpb.FieldDescriptorProto_LABEL_REQUIRED
	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	repeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	typeString := descriptorpb.FieldDescriptorProto_TYPE_STRING
	typeInt32 := descriptorpb.FieldDescriptorProto_TYPE_INT32
	typeBool := descriptorpb.FieldDescriptorProto_TYPE_BOOL
	typeMessage := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	typeEnum := descriptorpb.FieldDescriptorProto_TYPE_ENUM

	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    ptr("test/v1/test.proto"),
		Package: ptr("test.v1"),
		Syntax:  ptr("proto2"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: ptr("State"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: ptr("STATE_UNSPECIFIED"), Number: ptr[int32](0)},
				{Name: ptr("STATE_OPEN"), Number: ptr[int32](1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: ptr("Child"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:     ptr("label"),
					JsonName: ptr("label"),
					Number:   ptr[int32](1),
					Label:    &optional,
					Type:     &typeString,
				}},
			},
			{
				Name: ptr("Request"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: ptr("id"), JsonName: ptr("id"), Number: ptr[int32](1), Label: &required, Type: &typeString},
					{Name: ptr("count"), JsonName: ptr("count"), Number: ptr[int32](2), Label: &optional, Type: &typeInt32},
					{Name: ptr("active"), JsonName: ptr("active"), Number: ptr[int32](3), Label: &optional, Type: &typeBool},
					{Name: ptr("tags"), JsonName: ptr("tags"), Number: ptr[int32](4), Label: &repeated, Type: &typeString},
					{Name: ptr("child"), JsonName: ptr("child"), Number: ptr[int32](5), Label: &optional, Type: &typeMessage, TypeName: ptr(".test.v1.Child")},
					{Name: ptr("state"), JsonName: ptr("state"), Number: ptr[int32](6), Label: &optional, Type: &typeEnum, TypeName: ptr(".test.v1.State")},
					{Name: ptr("query"), JsonName: ptr("query"), Number: ptr[int32](7), Label: &optional, Type: &typeString, OneofIndex: ptr[int32](0)},
					{Name: ptr("page"), JsonName: ptr("page"), Number: ptr[int32](8), Label: &optional, Type: &typeInt32, OneofIndex: ptr[int32](0)},
					{Name: ptr("attributes"), JsonName: ptr("attributes"), Number: ptr[int32](9), Label: &repeated, Type: &typeMessage, TypeName: ptr(".test.v1.Request.AttributesEntry")},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: ptr("selector")}},
				NestedType: []*descriptorpb.DescriptorProto{{
					Name: ptr("AttributesEntry"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{Name: ptr("key"), JsonName: ptr("key"), Number: ptr[int32](1), Label: &optional, Type: &typeString},
						{Name: ptr("value"), JsonName: ptr("value"), Number: ptr[int32](2), Label: &optional, Type: &typeString},
					},
					Options: &descriptorpb.MessageOptions{MapEntry: ptr(true)},
				}},
			},
		},
	}, nil)
	s.Require().NoError(err)
	s.file = fd
}

func (s *ConverterSuite) TestBuildsObjectSchemaForMessageFields() {
	msg := s.file.Messages().ByName("Request")

	got, err := gatewayschema.ForMessage(msg)

	s.Require().NoError(err)
	s.Equal("object", got["type"])
	s.ElementsMatch([]any{"id"}, got["required"])

	props := got["properties"].(map[string]any)
	s.Equal(map[string]any{"type": "string"}, props["id"])
	s.Equal(map[string]any{"type": "integer", "format": "int32"}, props["count"])
	s.Equal(map[string]any{"type": "boolean"}, props["active"])
	s.Equal(map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}, props["tags"])
	s.Equal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label": map[string]any{"type": "string"},
		},
	}, props["child"])
	s.Equal(map[string]any{
		"type": "string",
		"enum": []any{"STATE_UNSPECIFIED", "STATE_OPEN"},
	}, props["state"])
}

func (s *ConverterSuite) TestBuildsMapAndOneofFields() {
	msg := s.file.Messages().ByName("Request")
	got, err := gatewayschema.ForMessage(msg)

	s.Require().NoError(err)
	props := got["properties"].(map[string]any)
	s.Equal(map[string]any{
		"type":                 "object",
		"additionalProperties": map[string]any{"type": "string"},
	}, props["attributes"])
	s.Equal(map[string]any{"type": "string"}, props["query"])
	s.Equal(map[string]any{"type": "integer", "format": "int32"}, props["page"])
}

func ptr[T any](v T) *T {
	return &v
}
