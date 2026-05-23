package annotations

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

var toolExtensionNames = []protoreflect.FullName{
	"grpcmcpgateway.v1.tool",
	"grpcmcpgateway.v1.Tool",
}

type ToolMetadata struct {
	Name        string
	Description string
}

func ForMethod(method protoreflect.MethodDescriptor) ToolMetadata {
	meta := fallback(method)
	if method == nil || method.Options() == nil {
		return meta
	}
	options := method.Options().(*descriptorpb.MethodOptions)

	if meta, ok := fromRegisteredExtension(meta, options); ok {
		return meta
	}

	ext := findToolExtension(method.ParentFile())
	if ext == nil {
		return meta
	}

	extType := dynamicpb.NewExtensionType(ext)
	if !proto.HasExtension(options, extType) {
		msg := dynamicpb.NewMessage(ext.Message())
		if !unmarshalUnknownExtension(options.ProtoReflect().GetUnknown(), ext, msg) {
			return meta
		}
		return applyToolMessage(meta, msg)
	}
	value := proto.GetExtension(options, extType)
	protoMsg, ok := value.(proto.Message)
	if !ok || protoMsg == nil {
		return meta
	}
	msg := protoMsg.ProtoReflect()
	if !msg.IsValid() {
		return meta
	}

	return applyToolMessage(meta, msg)
}

func fromRegisteredExtension(meta ToolMetadata, options *descriptorpb.MethodOptions) (ToolMetadata, bool) {
	for _, name := range toolExtensionNames {
		extType, err := protoregistry.GlobalTypes.FindExtensionByName(name)
		if err != nil || !proto.HasExtension(options, extType) {
			continue
		}
		value := proto.GetExtension(options, extType)
		protoMsg, ok := value.(proto.Message)
		if !ok || protoMsg == nil {
			continue
		}
		return applyToolMessage(meta, protoMsg.ProtoReflect()), true
	}
	return meta, false
}

func applyToolMessage(meta ToolMetadata, msg protoreflect.Message) ToolMetadata {
	fields := msg.Descriptor().Fields()
	if name := stringField(msg, fields.ByName("name")); name != "" {
		meta.Name = name
	}
	if description := stringField(msg, fields.ByName("description")); description != "" {
		meta.Description = description
	}
	return meta
}

func unmarshalUnknownExtension(raw protoreflect.RawFields, ext protoreflect.ExtensionDescriptor, msg *dynamicpb.Message) bool {
	for len(raw) > 0 {
		number, typ, tagLen := protowire.ConsumeTag(raw)
		if tagLen < 0 {
			return false
		}
		raw = raw[tagLen:]
		valueLen := protowire.ConsumeFieldValue(number, typ, raw)
		if valueLen < 0 {
			return false
		}
		if number == protowire.Number(ext.Number()) && typ == protowire.BytesType {
			bytes, n := protowire.ConsumeBytes(raw[:valueLen])
			if n < 0 {
				return false
			}
			return proto.Unmarshal(bytes, msg) == nil
		}
		raw = raw[valueLen:]
	}
	return false
}

func fallback(method protoreflect.MethodDescriptor) ToolMetadata {
	if method == nil {
		return ToolMetadata{}
	}
	return ToolMetadata{
		Name:        string(method.Name()),
		Description: fmt.Sprintf("Calls %s/%s", method.Parent().FullName(), method.Name()),
	}
}

func stringField(msg protoreflect.Message, field protoreflect.FieldDescriptor) string {
	if field == nil || field.Kind() != protoreflect.StringKind || !msg.Has(field) {
		return ""
	}
	return msg.Get(field).String()
}

func findToolExtension(file protoreflect.FileDescriptor) protoreflect.ExtensionDescriptor {
	seen := map[string]bool{}
	return findToolExtensionInFile(file, seen)
}

func findToolExtensionInFile(file protoreflect.FileDescriptor, seen map[string]bool) protoreflect.ExtensionDescriptor {
	if file == nil || seen[file.Path()] {
		return nil
	}
	seen[file.Path()] = true

	extensions := file.Extensions()
	for i := 0; i < extensions.Len(); i++ {
		ext := extensions.Get(i)
		for _, name := range toolExtensionNames {
			if ext.FullName() == name {
				return ext
			}
		}
	}

	imports := file.Imports()
	for i := 0; i < imports.Len(); i++ {
		if ext := findToolExtensionInFile(imports.Get(i).FileDescriptor, seen); ext != nil {
			return ext
		}
	}
	return nil
}
