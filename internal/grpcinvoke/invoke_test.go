package grpcinvoke_test

import (
	"context"
	"net"
	"testing"

	"go.cadenya.com/mcp-grpc-gateway/internal/grpcinvoke"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type InvokeSuite struct {
	suite.Suite
	method  protoreflect.MethodDescriptor
	client  *grpc.ClientConn
	server  *grpc.Server
	gotTags []string
}

func TestInvokeSuite(t *testing.T) {
	suite.Run(t, new(InvokeSuite))
}

func (s *InvokeSuite) SetupTest() {
	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	repeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	typeString := descriptorpb.FieldDescriptorProto_TYPE_STRING
	typeInt32 := descriptorpb.FieldDescriptorProto_TYPE_INT32
	typeBool := descriptorpb.FieldDescriptorProto_TYPE_BOOL

	s.gotTags = nil
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    ptr("test/v1/echo.proto"),
		Package: ptr("test.v1"),
		Syntax:  ptr("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: ptr("EchoRequest"), Field: []*descriptorpb.FieldDescriptorProto{
				{Name: ptr("id"), JsonName: ptr("id"), Number: ptr[int32](1), Label: &optional, Type: &typeString},
				{Name: ptr("count"), JsonName: ptr("count"), Number: ptr[int32](2), Label: &optional, Type: &typeInt32},
				{Name: ptr("tags"), JsonName: ptr("tags"), Number: ptr[int32](3), Label: &repeated, Type: &typeString},
			}},
			{Name: ptr("EchoResponse"), Field: []*descriptorpb.FieldDescriptorProto{
				{Name: ptr("ok"), JsonName: ptr("ok"), Number: ptr[int32](1), Label: &optional, Type: &typeBool},
				{Name: ptr("id"), JsonName: ptr("id"), Number: ptr[int32](2), Label: &optional, Type: &typeString},
			}},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: ptr("EchoService"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       ptr("Echo"),
				InputType:  ptr(".test.v1.EchoRequest"),
				OutputType: ptr(".test.v1.EchoResponse"),
			}},
		}},
	}, nil)
	s.Require().NoError(err)
	s.method = fd.Services().ByName("EchoService").Methods().ByName("Echo")

	listener := bufconn.Listen(1024 * 1024)
	s.server = grpc.NewServer()
	s.server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.v1.EchoService",
		HandlerType: (*echoServer)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Echo",
			Handler: func(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				req := dynamicpb.NewMessage(s.method.Input())
				if err := dec(req); err != nil {
					return nil, err
				}
				tags := req.Get(s.method.Input().Fields().ByName("tags")).List()
				for i := 0; i < tags.Len(); i++ {
					s.gotTags = append(s.gotTags, tags.Get(i).String())
				}
				resp := dynamicpb.NewMessage(s.method.Output())
				resp.Set(s.method.Output().Fields().ByName("ok"), protoreflect.ValueOfBool(true))
				resp.Set(s.method.Output().Fields().ByName("id"), req.Get(s.method.Input().Fields().ByName("id")))
				return resp, nil
			},
		}},
	}, (*echoServerImpl)(nil))
	go func() {
		_ = s.server.Serve(listener)
	}()

	client, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithInsecure())
	s.Require().NoError(err)
	s.client = client
}

func (s *InvokeSuite) TearDownTest() {
	_ = s.client.Close()
	s.server.Stop()
}

func (s *InvokeSuite) TestInvokesUnaryMethodWithJSONArguments() {
	got, err := grpcinvoke.InvokeUnary(context.Background(), s.client, s.method, []byte(`{"id":"abc","count":3}`))

	s.Require().NoError(err)
	s.Equal(map[string]any{"ok": true, "id": "abc"}, got)
}

func (s *InvokeSuite) TestCoercesStringifiedArrayArguments() {
	// Models sometimes JSON-encode a nested array as a string; the strict
	// parse fails and the repaired payload must land the real values.
	got, err := grpcinvoke.InvokeUnary(context.Background(), s.client, s.method,
		[]byte(`{"id":"abc","tags":"[\"a\",\"b\"]"}`))

	s.Require().NoError(err)
	s.Equal(map[string]any{"ok": true, "id": "abc"}, got)
	s.Equal([]string{"a", "b"}, s.gotTags)
}

func (s *InvokeSuite) TestRejectsStringThatIsNotAnArray() {
	_, err := grpcinvoke.InvokeUnary(context.Background(), s.client, s.method,
		[]byte(`{"tags":"not an array"}`))

	s.Require().Error(err)
	s.Contains(err.Error(), "unmarshal arguments")
}

func (s *InvokeSuite) TestRejectsInvalidJSONArguments() {
	_, err := grpcinvoke.InvokeUnary(context.Background(), s.client, s.method, []byte(`{"missing":true}`))

	s.Require().Error(err)
	s.Contains(err.Error(), "unmarshal arguments")
}

type echoServer interface{}

type echoServerImpl struct{}

func ptr[T any](v T) *T {
	return &v
}
