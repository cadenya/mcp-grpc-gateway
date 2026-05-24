package schema

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func ForMessage(desc protoreflect.MessageDescriptor) (map[string]any, error) {
	if desc == nil {
		return nil, fmt.Errorf("message descriptor is nil")
	}
	return messageSchema(desc), nil
}

func messageSchema(desc protoreflect.MessageDescriptor) map[string]any {
	props := map[string]any{}
	required := []any{}

	fields := desc.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		props[string(field.JSONName())] = fieldSchema(field)
		if field.Cardinality() == protoreflect.Required {
			required = append(required, string(field.JSONName()))
		}
	}

	out := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func fieldSchema(field protoreflect.FieldDescriptor) map[string]any {
	if field.IsMap() {
		return map[string]any{
			"type":                 "object",
			"additionalProperties": fieldSchema(field.MapValue()),
		}
	}
	if field.IsList() {
		return map[string]any{
			"type":  "array",
			"items": scalarOrMessageSchema(field),
		}
	}
	return scalarOrMessageSchema(field)
}

func scalarOrMessageSchema(field protoreflect.FieldDescriptor) map[string]any {
	switch field.Kind() {
	case protoreflect.BoolKind:
		return map[string]any{"type": "boolean"}
	case protoreflect.EnumKind:
		if field.Enum().FullName() == "google.protobuf.NullValue" {
			return map[string]any{"type": "null"}
		}
		values := field.Enum().Values()
		enum := make([]any, 0, values.Len())
		for i := 0; i < values.Len(); i++ {
			enum = append(enum, string(values.Get(i).Name()))
		}
		return map[string]any{"type": "string", "enum": enum}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return map[string]any{"type": "integer", "format": "int32"}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return map[string]any{"type": "integer", "format": "uint32"}
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return map[string]any{"type": "integer", "format": "int64"}
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return map[string]any{"type": "integer", "format": "uint64"}
	case protoreflect.FloatKind:
		return map[string]any{"type": "number", "format": "float"}
	case protoreflect.DoubleKind:
		return map[string]any{"type": "number", "format": "double"}
	case protoreflect.BytesKind:
		return map[string]any{"type": "string", "contentEncoding": "base64"}
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if schema, ok := wellKnownMessageSchema(field.Message().FullName()); ok {
			return schema
		}
		return messageSchema(field.Message())
	default:
		return map[string]any{"type": "string"}
	}
}

func wellKnownMessageSchema(name protoreflect.FullName) (map[string]any, bool) {
	switch name {
	case "google.protobuf.Timestamp":
		return map[string]any{"type": "string", "format": "date-time"}, true
	case "google.protobuf.Duration", "google.protobuf.FieldMask":
		return map[string]any{"type": "string"}, true
	case "google.protobuf.DoubleValue", "google.protobuf.FloatValue":
		return map[string]any{"type": "number"}, true
	case "google.protobuf.Int32Value", "google.protobuf.UInt32Value":
		return map[string]any{"type": "integer", "format": "int32"}, true
	case "google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		return map[string]any{"type": "integer", "format": "int64"}, true
	case "google.protobuf.BoolValue":
		return map[string]any{"type": "boolean"}, true
	case "google.protobuf.StringValue":
		return map[string]any{"type": "string"}, true
	case "google.protobuf.BytesValue":
		return map[string]any{"type": "string", "contentEncoding": "base64"}, true
	case "google.protobuf.Struct":
		return map[string]any{"type": "object", "additionalProperties": true}, true
	case "google.protobuf.Value":
		return map[string]any{}, true
	case "google.protobuf.ListValue":
		return map[string]any{"type": "array", "items": map[string]any{}}, true
	default:
		return nil, false
	}
}
