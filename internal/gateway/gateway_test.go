package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	grpcmcpgatewayv1 "go.cadenya.com/mcp-grpc-gateway/gen/grpcmcpgateway/v1"
	"go.cadenya.com/mcp-grpc-gateway/internal/gateway"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
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

	fdProto := &descriptorpb.FileDescriptorProto{
		Name:    ptr("test/v1/echo.proto"),
		Package: ptr("test.v1"),
		Syntax:  ptr("proto3"),
		Dependency: []string{
			"grpcmcpgateway/v1/annotations.proto",
		},
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
			Name: ptr("Tool"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{Name: ptr("title"), JsonName: ptr("title"), Number: ptr[int32](4), Label: &optional, Type: &typeString},
				{Name: ptr("read_only_hint"), JsonName: ptr("readOnlyHint"), Number: ptr[int32](5), Label: &optional, Type: &typeBool, Proto3Optional: ptr(true), OneofIndex: ptr[int32](0)},
				{Name: ptr("destructive_hint"), JsonName: ptr("destructiveHint"), Number: ptr[int32](6), Label: &optional, Type: &typeBool, Proto3Optional: ptr(true), OneofIndex: ptr[int32](1)},
				{Name: ptr("idempotent_hint"), JsonName: ptr("idempotentHint"), Number: ptr[int32](7), Label: &optional, Type: &typeBool, Proto3Optional: ptr(true), OneofIndex: ptr[int32](2)},
				{Name: ptr("open_world_hint"), JsonName: ptr("openWorldHint"), Number: ptr[int32](8), Label: &optional, Type: &typeBool, Proto3Optional: ptr(true), OneofIndex: ptr[int32](3)},
			},
			OneofDecl: []*descriptorpb.OneofDescriptorProto{
				{Name: ptr("_read_only_hint")},
				{Name: ptr("_destructive_hint")},
				{Name: ptr("_idempotent_hint")},
				{Name: ptr("_open_world_hint")},
			},
		}},
		Extension: []*descriptorpb.FieldDescriptorProto{{
			Name:     ptr("tool"),
			Number:   ptr(grpcmcpgatewayv1.ToolExtensionNumber),
			Label:    &optional,
			Type:     ptr(descriptorpb.FieldDescriptorProto_TYPE_MESSAGE),
			TypeName: ptr(".grpcmcpgateway.v1.Tool"),
			Extendee: ptr(".google.protobuf.MethodOptions"),
			JsonName: ptr("tool"),
		}},
	}, baseFiles)
	s.Require().NoError(err)
	ext := toolFile.Extensions().ByName("tool")
	tool := dynamicpb.NewMessage(ext.Message())
	tool.Set(ext.Message().Fields().ByName("title"), protoreflect.ValueOfString("Echo request"))
	tool.Set(ext.Message().Fields().ByName("read_only_hint"), protoreflect.ValueOfBool(true))
	tool.Set(ext.Message().Fields().ByName("destructive_hint"), protoreflect.ValueOfBool(false))
	tool.Set(ext.Message().Fields().ByName("idempotent_hint"), protoreflect.ValueOfBool(true))
	tool.Set(ext.Message().Fields().ByName("open_world_hint"), protoreflect.ValueOfBool(false))
	toolBytes, err := proto.Marshal(tool)
	s.Require().NoError(err)
	unknown := protowire.AppendTag(nil, protowire.Number(ext.Number()), protowire.BytesType)
	unknown = protowire.AppendBytes(unknown, toolBytes)
	fdProto.Service[0].Method[0].Options = &descriptorpb.MethodOptions{}
	fdProto.Service[0].Method[0].Options.ProtoReflect().SetUnknown(unknown)
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		protodesc.ToFileDescriptorProto(descriptorpb.File_google_protobuf_descriptor_proto),
		protodesc.ToFileDescriptorProto(toolFile),
		fdProto,
	}})
	s.Require().NoError(err)
	fd, err := files.FindFileByPath("test/v1/echo.proto")
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

func (s *GatewaySuite) TestRegistersToolAnnotations() {
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
	s.Require().NotNil(tools[0].Annotations)
	s.Equal("Echo request", tools[0].Annotations.Title)
	s.True(tools[0].Annotations.ReadOnlyHint)
	s.Require().NotNil(tools[0].Annotations.DestructiveHint)
	s.False(*tools[0].Annotations.DestructiveHint)
	s.True(tools[0].Annotations.IdempotentHint)
	s.Require().NotNil(tools[0].Annotations.OpenWorldHint)
	s.False(*tools[0].Annotations.OpenWorldHint)
}

func (s *GatewaySuite) TestCanRequireToolAnnotations() {
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.other, gateway.WithRequireToolAnnotations(true)))
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

func (s *GatewaySuite) TestAppliesServiceToolPrefixToToolNames() {
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	seen := map[string]string{}

	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.service, gateway.WithRegisteredToolNames(seen), gateway.WithServiceToolPrefix("echo_")))
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.collide, gateway.WithRegisteredToolNames(seen), gateway.WithServiceToolPrefix("collision_")))
	session := s.connect(server)
	defer session.Close()

	var names []string
	for tool, err := range session.Tools(context.Background(), nil) {
		s.Require().NoError(err)
		names = append(names, tool.Name)
	}
	s.ElementsMatch([]string{"echo_Echo", "collision_Echo"}, names)
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

func (s *GatewaySuite) TestToolCallTimeoutAddsDeadlineToInvocationContext() {
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.service, gateway.WithTimeout(5*time.Second)))
	session := s.connect(server)
	defer session.Close()

	got, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "Echo",
		Arguments: map[string]any{"id": "abc"},
	})

	s.Require().NoError(err)
	s.False(got.IsError)
	s.True(s.conn.hadDeadline)
}

func (s *GatewaySuite) TestToolCallTimeoutReturnsToolErrorWhenDeadlineExpires() {
	s.conn.waitForContext = true
	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "test"}, nil)
	s.Require().NoError(gateway.RegisterTools(server, s.conn, s.service, gateway.WithTimeout(time.Nanosecond)))
	session := s.connect(server)
	defer session.Close()

	got, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "Echo",
		Arguments: map[string]any{"id": "abc"},
	})

	s.Require().NoError(err)
	s.True(got.IsError)
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
	method         protoreflect.MethodDescriptor
	calledMethod   string
	hadDeadline    bool
	waitForContext bool
}

func (f *fakeConn) Invoke(ctx context.Context, method string, args any, reply any, _ ...grpc.CallOption) error {
	f.calledMethod = method
	_, f.hadDeadline = ctx.Deadline()
	if f.waitForContext {
		<-ctx.Done()
		return ctx.Err()
	}
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
