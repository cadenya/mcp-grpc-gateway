package toolregistry_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	grpcmcpgatewayv1 "go.cadenya.com/mcp-grpc-gateway/gen/grpcmcpgateway/v1"
	"go.cadenya.com/mcp-grpc-gateway/internal/testpb"
	"go.cadenya.com/mcp-grpc-gateway/internal/toolregistry"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestBuildListsUnaryAnnotatedTools(t *testing.T) {
	service := testpb.File_functional_v1_greeter_proto.Services().ByName("GreeterService")

	registry, err := toolregistry.Build(toolregistry.BuildOptions{
		Conn:                    &fakeConn{method: service.Methods().ByName("Greet")},
		Services:                []protoreflect.ServiceDescriptor{service},
		Logger:                  slog.Default(),
		RequireToolAnnotations:  true,
		RegisteredToolNameOwner: map[string]string{},
	})

	require.NoError(t, err)
	tools := registry.Tools()
	require.Len(t, tools, 1)
	require.Equal(t, "greet_user", tools[0].Name)
	require.Equal(t, "Greets a user by name", tools[0].Description)
	require.Equal(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}, tools[0].InputSchema)
}

func TestCallReturnsStructuredContent(t *testing.T) {
	service := testpb.File_functional_v1_greeter_proto.Services().ByName("GreeterService")
	conn := &fakeConn{method: service.Methods().ByName("Greet")}
	registry, err := toolregistry.Build(toolregistry.BuildOptions{
		Conn:                    conn,
		Services:                []protoreflect.ServiceDescriptor{service},
		Logger:                  slog.Default(),
		RegisteredToolNameOwner: map[string]string{},
	})
	require.NoError(t, err)

	result, err := registry.Call(context.Background(), "greet_user", []byte(`{"name":"Ada"}`))

	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": `{"greeting":"Hello, Ada"}`},
		},
		"structuredContent": map[string]any{"greeting": "Hello, Ada"},
	}, result)
	require.Equal(t, "/functional.v1.GreeterService/Greet", conn.calledMethod)
}

func TestCallReturnsToolErrorForInvocationFailure(t *testing.T) {
	service := testpb.File_functional_v1_greeter_proto.Services().ByName("GreeterService")
	registry, err := toolregistry.Build(toolregistry.BuildOptions{
		Conn:                    &fakeConn{method: service.Methods().ByName("Greet"), err: fmt.Errorf("backend unavailable")},
		Services:                []protoreflect.ServiceDescriptor{service},
		Logger:                  slog.Default(),
		RegisteredToolNameOwner: map[string]string{},
	})
	require.NoError(t, err)

	result, err := registry.Call(context.Background(), "greet_user", []byte(`{"name":"Ada"}`))

	require.NoError(t, err)
	require.Equal(t, true, result["isError"])
	require.Contains(t, result["content"], map[string]any{
		"type": "text",
		"text": "invoke /functional.v1.GreeterService/Greet: backend unavailable",
	})
}

func TestCallRendersContentTemplateAsTextOnlyContent(t *testing.T) {
	service := annotatedTemplateService(t, "Session: {{ .content }}")
	registry, err := toolregistry.Build(toolregistry.BuildOptions{
		Conn:                    &contentConn{method: service.Methods().ByName("Echo"), content: "hello"},
		Services:                []protoreflect.ServiceDescriptor{service},
		Logger:                  slog.Default(),
		RegisteredToolNameOwner: map[string]string{},
	})
	require.NoError(t, err)

	result, err := registry.Call(context.Background(), "Echo", []byte(`{"id":"x"}`))

	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "Session: hello"},
		},
	}, result)
}

func TestBuildSkipsInvalidContentTemplate(t *testing.T) {
	service := annotatedTemplateService(t, "{{ .content ")
	registry, err := toolregistry.Build(toolregistry.BuildOptions{
		Conn:                    &contentConn{method: service.Methods().ByName("Echo"), content: "hello"},
		Services:                []protoreflect.ServiceDescriptor{service},
		Logger:                  slog.Default(),
		RegisteredToolNameOwner: map[string]string{},
	})

	require.NoError(t, err)
	require.Empty(t, registry.Tools())
}

type fakeConn struct {
	method       protoreflect.MethodDescriptor
	calledMethod string
	err          error
}

type contentConn struct {
	method  protoreflect.MethodDescriptor
	content string
}

func (c *contentConn) Invoke(ctx context.Context, method string, args any, reply any, _ ...grpc.CallOption) error {
	resp := reply.(*dynamicpb.Message)
	resp.Set(c.method.Output().Fields().ByName("content"), protoreflect.ValueOfString(c.content))
	return nil
}

func (c *contentConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, fmt.Errorf("streaming is not supported")
}

func annotatedTemplateService(t *testing.T, contentTemplate string) protoreflect.ServiceDescriptor {
	t.Helper()
	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	typeString := descriptorpb.FieldDescriptorProto_TYPE_STRING

	fdProto := &descriptorpb.FileDescriptorProto{
		Name:       ptr("tmpl/v1/echo.proto"),
		Package:    ptr("tmpl.v1"),
		Syntax:     ptr("proto3"),
		Dependency: []string{"grpcmcpgateway/v1/annotations.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: ptr("EchoRequest"), Field: []*descriptorpb.FieldDescriptorProto{
				{Name: ptr("id"), JsonName: ptr("id"), Number: ptr[int32](1), Label: &optional, Type: &typeString},
			}},
			{Name: ptr("EchoResponse"), Field: []*descriptorpb.FieldDescriptorProto{
				{Name: ptr("content"), JsonName: ptr("content"), Number: ptr[int32](1), Label: &optional, Type: &typeString},
			}},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: ptr("EchoService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: ptr("Echo"), InputType: ptr(".tmpl.v1.EchoRequest"), OutputType: ptr(".tmpl.v1.EchoResponse"), Options: &descriptorpb.MethodOptions{}},
			},
		}},
	}

	toolBytes, err := proto.Marshal(&grpcmcpgatewayv1.Tool{Name: "Echo", ContentTemplate: contentTemplate})
	require.NoError(t, err)
	unknown := protowire.AppendTag(nil, protowire.Number(grpcmcpgatewayv1.ToolExtensionNumber), protowire.BytesType)
	unknown = protowire.AppendBytes(unknown, toolBytes)
	fdProto.Service[0].Method[0].Options.ProtoReflect().SetUnknown(unknown)

	fd, err := protodesc.NewFile(fdProto, protoregistry.GlobalFiles)
	require.NoError(t, err)
	return fd.Services().ByName("EchoService")
}

func ptr[T any](v T) *T {
	return &v
}

func (f *fakeConn) Invoke(ctx context.Context, method string, args any, reply any, _ ...grpc.CallOption) error {
	f.calledMethod = method
	if f.err != nil {
		return f.err
	}
	req := args.(proto.Message).ProtoReflect()
	resp := reply.(*dynamicpb.Message)
	resp.Set(f.method.Output().Fields().ByName("greeting"), protoreflect.ValueOfString("Hello, "+req.Get(f.method.Input().Fields().ByName("name")).String()))
	return nil
}

func (f *fakeConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, fmt.Errorf("streaming is not supported")
}
