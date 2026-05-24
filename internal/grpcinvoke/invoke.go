package grpcinvoke

import (
	"context"
	"encoding/json"
	"fmt"

	"cadenya.com/mcp-grpc-gateway/internal/forwardmetadata"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

var tracer = otel.Tracer("cadenya.com/mcp-grpc-gateway/internal/grpcinvoke")

func InvokeUnary(ctx context.Context, conn grpc.ClientConnInterface, method protoreflect.MethodDescriptor, args []byte) (map[string]any, error) {
	if conn == nil {
		return nil, fmt.Errorf("grpc connection is nil")
	}
	if method == nil {
		return nil, fmt.Errorf("method descriptor is nil")
	}
	ctx, span := tracer.Start(ctx, "grpcinvoke.invoke_unary", trace.WithAttributes(
		attribute.String("rpc.system", "grpc"),
		attribute.String("rpc.service", string(method.Parent().FullName())),
		attribute.String("rpc.method", string(method.Name())),
	))
	defer span.End()

	if method.IsStreamingClient() || method.IsStreamingServer() {
		return nil, spanError(span, fmt.Errorf("method %s is streaming; only unary methods are supported", method.FullName()))
	}

	req := dynamicpb.NewMessage(method.Input())
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(args, req); err != nil {
		return nil, spanError(span, fmt.Errorf("unmarshal arguments: %w", err))
	}

	resp := dynamicpb.NewMessage(method.Output())
	fullMethod := fmt.Sprintf("/%s/%s", method.Parent().FullName(), method.Name())
	ctx = forwardmetadata.AppendToOutgoingContext(ctx)
	if err := conn.Invoke(ctx, fullMethod, req, resp); err != nil {
		return nil, spanError(span, fmt.Errorf("invoke %s: %w", fullMethod, err))
	}

	raw, err := (protojson.MarshalOptions{UseProtoNames: false}).Marshal(resp)
	if err != nil {
		return nil, spanError(span, fmt.Errorf("marshal response: %w", err))
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, spanError(span, fmt.Errorf("decode response json: %w", err))
	}
	return out, nil
}

func spanError(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}
