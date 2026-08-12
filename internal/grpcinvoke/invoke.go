package grpcinvoke

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.cadenya.com/mcp-grpc-gateway/internal/forwardmetadata"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

var tracer = otel.Tracer("go.cadenya.com/mcp-grpc-gateway/internal/grpcinvoke")

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
		// Models sometimes JSON-encode a nested array or object as a string
		// ("args": "[\"-n\", \"foo\"]"). Unwrap such values where the field
		// expects that shape and retry once; valid payloads never get here.
		repaired, changed := unwrapStringifiedFields(args, method.Input())
		if !changed {
			return nil, spanError(span, fmt.Errorf("unmarshal arguments: %w", err))
		}
		if retryErr := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(repaired, req); retryErr != nil {
			return nil, spanError(span, fmt.Errorf("unmarshal arguments: %w", err))
		}
	}

	resp := dynamicpb.NewMessage(method.Output())
	fullMethod := fmt.Sprintf("/%s/%s", method.Parent().FullName(), method.Name())
	ctx = forwardmetadata.AppendToOutgoingContext(ctx)
	if err := conn.Invoke(ctx, fullMethod, req, resp); err != nil {
		return nil, spanError(span, fmt.Errorf("invoke %s: %w", fullMethod, err))
	}

	raw, err := (protojson.MarshalOptions{UseProtoNames: false, EmitDefaultValues: true}).Marshal(resp)
	if err != nil {
		return nil, spanError(span, fmt.Errorf("marshal response: %w", err))
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, spanError(span, fmt.Errorf("decode response json: %w", err))
	}
	return out, nil
}

// unwrapStringifiedFields returns args with top-level string values replaced
// by their parsed JSON content when the target field expects a list, map, or
// message and the string itself is valid JSON of that shape.
func unwrapStringifiedFields(args []byte, input protoreflect.MessageDescriptor) ([]byte, bool) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(args, &payload); err != nil {
		return args, false
	}
	changed := false
	fields := input.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		for _, name := range []string{field.JSONName(), string(field.Name())} {
			raw, ok := payload[name]
			if !ok || len(raw) == 0 || raw[0] != '"' {
				continue
			}
			var inner string
			if err := json.Unmarshal(raw, &inner); err != nil {
				continue
			}
			trimmed := strings.TrimSpace(inner)
			wantsList := field.IsList() && strings.HasPrefix(trimmed, "[")
			wantsObject := (field.IsMap() || (!field.IsList() && field.Kind() == protoreflect.MessageKind)) && strings.HasPrefix(trimmed, "{")
			if !wantsList && !wantsObject {
				continue
			}
			var validated json.RawMessage
			if err := json.Unmarshal([]byte(trimmed), &validated); err != nil {
				continue
			}
			payload[name] = validated
			changed = true
			break
		}
	}
	if !changed {
		return args, false
	}
	repaired, err := json.Marshal(payload)
	if err != nil {
		return args, false
	}
	return repaired, true
}

func spanError(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}
