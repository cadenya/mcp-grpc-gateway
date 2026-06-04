package gateway_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	grpcmcpgatewayv1 "go.cadenya.com/mcp-grpc-gateway/gen/grpcmcpgateway/v1"
	"go.cadenya.com/mcp-grpc-gateway/internal/gateway"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type ContentTemplateSuite struct {
	suite.Suite
}

func TestContentTemplateSuite(t *testing.T) {
	suite.Run(t, new(ContentTemplateSuite))
}

// annotatedService builds a one-method service whose Echo RPC carries a tool
// annotation with the given content_template. The Echo response has a single
// string field "content".
func (s *ContentTemplateSuite) annotatedService(contentTemplate string) protoreflect.ServiceDescriptor {
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
	s.Require().NoError(err)
	unknown := protowire.AppendTag(nil, protowire.Number(grpcmcpgatewayv1.ToolExtensionNumber), protowire.BytesType)
	unknown = protowire.AppendBytes(unknown, toolBytes)
	fdProto.Service[0].Method[0].Options.ProtoReflect().SetUnknown(unknown)

	fd, err := protodesc.NewFile(fdProto, protoregistry.GlobalFiles)
	s.Require().NoError(err)
	return fd.Services().ByName("EchoService")
}

func (s *ContentTemplateSuite) connect(server *mcp.Server) *mcp.ClientSession {
	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	_, err := server.Connect(ctx, t1, nil)
	s.Require().NoError(err)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	s.Require().NoError(err)
	return session
}

func (s *ContentTemplateSuite) TestRendersTextOnlyContent() {
	svc := s.annotatedService("Session: {{ .content }}")
	conn := &contentConn{method: svc.Methods().ByName("Echo"), content: "hello"}
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	s.Require().NoError(gateway.RegisterTools(server, conn, svc))
	session := s.connect(server)
	defer session.Close()

	got, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "Echo", Arguments: map[string]any{"id": "x"}})

	s.Require().NoError(err)
	s.False(got.IsError)
	s.Nil(got.StructuredContent)
	s.Require().Len(got.Content, 1)
	text := got.Content[0].(*mcp.TextContent)
	s.Equal("Session: hello", text.Text)
}

func (s *ContentTemplateSuite) TestTemplateExecutionErrorReturnsToolError() {
	svc := s.annotatedService("{{ .missing }}")
	conn := &contentConn{method: svc.Methods().ByName("Echo"), content: "hello"}
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	s.Require().NoError(gateway.RegisterTools(server, conn, svc))
	session := s.connect(server)
	defer session.Close()

	got, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "Echo", Arguments: map[string]any{"id": "x"}})

	s.Require().NoError(err)
	s.True(got.IsError)
}

func (s *ContentTemplateSuite) TestInvalidTemplateSkipsRegistrationAndWarns() {
	svc := s.annotatedService("{{ .content ")
	conn := &contentConn{method: svc.Methods().ByName("Echo"), content: "hello"}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	s.Require().NoError(gateway.RegisterTools(server, conn, svc, gateway.WithLogger(logger)))
	session := s.connect(server)
	defer session.Close()

	var tools []*mcp.Tool
	for tool, err := range session.Tools(context.Background(), nil) {
		s.Require().NoError(err)
		tools = append(tools, tool)
	}

	s.Empty(tools)
	s.Contains(logs.String(), "invalid tool content template")
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
