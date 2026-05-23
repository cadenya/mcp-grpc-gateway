package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"cadenya.com/mcp-grpc-gateway/internal/gateway"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type GatewaySuite struct {
	suite.Suite
	service protoreflect.ServiceDescriptor
	conn    *fakeConn
}

func TestGatewaySuite(t *testing.T) {
	suite.Run(t, new(GatewaySuite))
}

func (s *GatewaySuite) SetupTest() {
	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	typeString := descriptorpb.FieldDescriptorProto_TYPE_STRING
	typeBool := descriptorpb.FieldDescriptorProto_TYPE_BOOL

	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    ptr("test/v1/echo.proto"),
		Package: ptr("test.v1"),
		Syntax:  ptr("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: ptr("EchoRequest"), Field: []*descriptorpb.FieldDescriptorProto{
				{Name: ptr("id"), JsonName: ptr("id"), Number: ptr[int32](1), Label: &optional, Type: &typeString},
			}},
			{Name: ptr("EchoResponse"), Field: []*descriptorpb.FieldDescriptorProto{
				{Name: ptr("ok"), JsonName: ptr("ok"), Number: ptr[int32](1), Label: &optional, Type: &typeBool},
				{Name: ptr("id"), JsonName: ptr("id"), Number: ptr[int32](2), Label: &optional, Type: &typeString},
			}},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: ptr("EchoService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: ptr("Echo"), InputType: ptr(".test.v1.EchoRequest"), OutputType: ptr(".test.v1.EchoResponse")},
				{Name: ptr("Watch"), InputType: ptr(".test.v1.EchoRequest"), OutputType: ptr(".test.v1.EchoResponse"), ServerStreaming: ptr(true)},
			},
		}},
	}, nil)
	s.Require().NoError(err)
	s.service = fd.Services().ByName("EchoService")
	s.conn = &fakeConn{method: s.service.Methods().ByName("Echo")}
}

func (s *GatewaySuite) TestRegistersUnaryRPCsAsMCPTools() {
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.service))
	session := s.connect(server)
	defer session.Close()

	var tools []*mcp.Tool
	for tool, err := range session.Tools(context.Background(), nil) {
		s.Require().NoError(err)
		tools = append(tools, tool)
	}

	s.Require().Len(tools, 1)
	s.Equal("Echo", tools[0].Name)
	s.Equal("Calls test.v1.EchoService/Echo", tools[0].Description)
	s.Equal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
		},
	}, normalizeSchema(s.T(), tools[0].InputSchema))
}

func (s *GatewaySuite) TestToolCallInvokesGRPCAndReturnsStructuredContent() {
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.service))
	session := s.connect(server)
	defer session.Close()

	got, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "Echo",
		Arguments: map[string]any{"id": "abc"},
	})

	s.Require().NoError(err)
	s.False(got.IsError)
	s.Equal(map[string]any{"ok": true, "id": "abc"}, got.StructuredContent)
	s.Require().Len(got.Content, 1)
	text := got.Content[0].(*mcp.TextContent)
	s.JSONEq(`{"id":"abc","ok":true}`, text.Text)
	s.Equal("/test.v1.EchoService/Echo", s.conn.calledMethod)
}

func (s *GatewaySuite) connect(server *mcp.Server) *mcp.ClientSession {
	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	_, err := server.Connect(ctx, t1, nil)
	s.Require().NoError(err)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	s.Require().NoError(err)
	return session
}

type fakeConn struct {
	method       protoreflect.MethodDescriptor
	calledMethod string
}

func (f *fakeConn) Invoke(_ context.Context, method string, args any, reply any, _ ...grpc.CallOption) error {
	f.calledMethod = method
	req := args.(proto.Message).ProtoReflect()
	resp := reply.(*dynamicpb.Message)
	resp.Set(f.method.Output().Fields().ByName("ok"), protoreflect.ValueOfBool(true))
	resp.Set(f.method.Output().Fields().ByName("id"), req.Get(f.method.Input().Fields().ByName("id")))
	return nil
}

func (f *fakeConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, fmt.Errorf("streaming is not supported")
}

func normalizeSchema(t *testing.T, schema any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func ptr[T any](v T) *T {
	return &v
}
