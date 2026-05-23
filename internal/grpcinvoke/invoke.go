package grpcinvoke

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func InvokeUnary(ctx context.Context, conn grpc.ClientConnInterface, method protoreflect.MethodDescriptor, args []byte) (map[string]any, error) {
	if conn == nil {
		return nil, fmt.Errorf("grpc connection is nil")
	}
	if method == nil {
		return nil, fmt.Errorf("method descriptor is nil")
	}
	if method.IsStreamingClient() || method.IsStreamingServer() {
		return nil, fmt.Errorf("method %s is streaming; only unary methods are supported", method.FullName())
	}

	req := dynamicpb.NewMessage(method.Input())
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(args, req); err != nil {
		return nil, fmt.Errorf("unmarshal arguments: %w", err)
	}

	resp := dynamicpb.NewMessage(method.Output())
	fullMethod := fmt.Sprintf("/%s/%s", method.Parent().FullName(), method.Name())
	if err := conn.Invoke(ctx, fullMethod, req, resp); err != nil {
		return nil, fmt.Errorf("invoke %s: %w", fullMethod, err)
	}

	raw, err := (protojson.MarshalOptions{UseProtoNames: false}).Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal response: %w", err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response json: %w", err)
	}
	return out, nil
}
