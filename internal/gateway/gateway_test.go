package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	"go.cadenya.com/mcp-grpc-gateway/internal/gateway"
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
	other   protoreflect.ServiceDescriptor
	collide protoreflect.ServiceDescriptor
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
		}, {
			Name: ptr("OtherService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: ptr("Ping"), InputType: ptr(".test.v1.EchoRequest"), OutputType: ptr(".test.v1.EchoResponse")},
			},
		}, {
			Name: ptr("CollisionService"),
			Method: []*descriptorpb.MethodDescriptorProto{
				{Name: ptr("Echo"), InputType: ptr(".test.v1.EchoRequest"), OutputType: ptr(".test.v1.EchoResponse")},
			},
		}},
	}, nil)
	s.Require().NoError(err)
	s.service = fd.Services().ByName("EchoService")
	s.other = fd.Services().ByName("OtherService")
	s.collide = fd.Services().ByName("CollisionService")
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

func (s *GatewaySuite) TestCanRequireToolAnnotations() {
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.service, gateway.WithRequireToolAnnotations(true)))
	session := s.connect(server)
	defer session.Close()

	var tools []*mcp.Tool
	for tool, err := range session.Tools(context.Background(), nil) {
		s.Require().NoError(err)
		tools = append(tools, tool)
	}

	s.Empty(tools)
}

func (s *GatewaySuite) TestRegistersToolsFromMultipleServices() {
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	seen := map[string]string{}
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.service, gateway.WithRegisteredToolNames(seen)))
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.other, gateway.WithRegisteredToolNames(seen)))
	session := s.connect(server)
	defer session.Close()

	var names []string
	for tool, err := range session.Tools(context.Background(), nil) {
		s.Require().NoError(err)
		names = append(names, tool.Name)
	}
	s.ElementsMatch([]string{"Echo", "Ping"}, names)
}

func (s *GatewaySuite) TestSkipsToolNameCollisionsAndWarns() {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	seen := map[string]string{}

	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.service, gateway.WithRegisteredToolNames(seen), gateway.WithLogger(logger)))
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.collide, gateway.WithRegisteredToolNames(seen), gateway.WithLogger(logger)))

	session := s.connect(server)
	defer session.Close()

	var tools []*mcp.Tool
	for tool, err := range session.Tools(context.Background(), nil) {
		s.Require().NoError(err)
		tools = append(tools, tool)
	}
	s.Require().Len(tools, 1)
	s.Contains(logs.String(), "tool name collision")
	s.Contains(logs.String(), "grpc_service=test.v1.CollisionService")
	s.Contains(logs.String(), "tool_name=Echo")
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

func (s *GatewaySuite) TestNewServerUsesRuntimeImplementationFields() {
	server := gateway.NewServer(gateway.ServerMetadata{
		Name:         "runtime-gateway",
		Title:        "Runtime Gateway",
		Version:      "1.2.3",
		Instructions: "Use the runtime-configured MCP server.",
		WebsiteURL:   "https://example.com/runtime",
	})
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.service))
	session := s.connect(server)
	defer session.Close()

	init := session.InitializeResult()
	s.Require().NotNil(init)
	s.Equal("runtime-gateway", init.ServerInfo.Name)
	s.Equal("Runtime Gateway", init.ServerInfo.Title)
	s.Equal("1.2.3", init.ServerInfo.Version)
	s.Equal("https://example.com/runtime", init.ServerInfo.WebsiteURL)
	s.Equal("Use the runtime-configured MCP server.", init.Instructions)
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
