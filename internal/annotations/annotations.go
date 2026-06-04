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
}

var serverExtensionNames = []protoreflect.FullName{
	"grpcmcpgateway.v1.server",
}

var serviceExtensionNames = []protoreflect.FullName{
	"grpcmcpgateway.v1.service",
}

type ToolMetadata struct {
	Name            string
	Description     string
	ContentTemplate string
	Annotated       bool
}

type ServerMetadata struct {
	Name         string
	Title        string
	Version      string
	Instructions string
	WebsiteURL   string
	ToolPrefix   string
}

func ForService(service protoreflect.ServiceDescriptor) ServerMetadata {
	meta := serverFallback(service)
	if service == nil || service.Options() == nil {
		return meta
	}
	options := service.Options().(*descriptorpb.ServiceOptions)

	if serviceMeta, ok := fromRegisteredServiceExtension(meta, options); ok {
		meta = serviceMeta
	}
	if ext := findExtension(service.ParentFile(), serviceExtensionNames); ext != nil {
		extType := dynamicpb.NewExtensionType(ext)
		if !proto.HasExtension(options, extType) {
			msg := dynamicpb.NewMessage(ext.Message())
			if unmarshalUnknownExtension(options.ProtoReflect().GetUnknown(), ext, msg) {
				meta = applyServiceMessage(meta, msg)
			}
		} else if value := proto.GetExtension(options, extType); value != nil {
			if protoMsg, ok := value.(proto.Message); ok && protoMsg != nil {
				meta = applyServiceMessage(meta, protoMsg.ProtoReflect())
			}
		}
	}

	if meta, ok := fromRegisteredServerExtension(meta, options); ok {
		return meta
	}

	ext := findExtension(service.ParentFile(), serverExtensionNames)
	if ext == nil {
		return meta
	}
	extType := dynamicpb.NewExtensionType(ext)
	if !proto.HasExtension(options, extType) {
		msg := dynamicpb.NewMessage(ext.Message())
		if !unmarshalUnknownExtension(options.ProtoReflect().GetUnknown(), ext, msg) {
			return meta
		}
		return applyServerMessage(meta, msg)
	}
	value := proto.GetExtension(options, extType)
	protoMsg, ok := value.(proto.Message)
	if !ok || protoMsg == nil {
		return meta
	}
	return applyServerMessage(meta, protoMsg.ProtoReflect())
}

func ForMethod(method protoreflect.MethodDescriptor) ToolMetadata {
	meta := fallback(method)
	if method == nil || method.Options() == nil {
		return meta
	}
	options := method.Options().(*descriptorpb.MethodOptions)

	if meta, ok := fromRegisteredExtension(meta, options); ok {
		meta.Annotated = true
		return meta
	}

	ext := findExtension(method.ParentFile(), toolExtensionNames)
	if ext == nil {
		return meta
	}

	extType := dynamicpb.NewExtensionType(ext)
	if !proto.HasExtension(options, extType) {
		msg := dynamicpb.NewMessage(ext.Message())
		if !unmarshalUnknownExtension(options.ProtoReflect().GetUnknown(), ext, msg) {
			return meta
		}
		meta = applyToolMessage(meta, msg)
		meta.Annotated = true
		return meta
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

	meta = applyToolMessage(meta, msg)
	meta.Annotated = true
	return meta
}

func fromRegisteredServerExtension(meta ServerMetadata, options *descriptorpb.ServiceOptions) (ServerMetadata, bool) {
	for _, name := range serverExtensionNames {
		extType, err := protoregistry.GlobalTypes.FindExtensionByName(name)
		if err != nil || !proto.HasExtension(options, extType) {
			continue
		}
		value := proto.GetExtension(options, extType)
		protoMsg, ok := value.(proto.Message)
		if !ok || protoMsg == nil {
			continue
		}
		return applyServerMessage(meta, protoMsg.ProtoReflect()), true
	}
	return meta, false
}

func fromRegisteredServiceExtension(meta ServerMetadata, options *descriptorpb.ServiceOptions) (ServerMetadata, bool) {
	for _, name := range serviceExtensionNames {
		extType, err := protoregistry.GlobalTypes.FindExtensionByName(name)
		if err != nil || !proto.HasExtension(options, extType) {
			continue
		}
		value := proto.GetExtension(options, extType)
		protoMsg, ok := value.(proto.Message)
		if !ok || protoMsg == nil {
			continue
		}
		return applyServiceMessage(meta, protoMsg.ProtoReflect()), true
	}
	return meta, false
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
	if contentTemplate := stringField(msg, fields.ByName("content_template")); contentTemplate != "" {
		meta.ContentTemplate = contentTemplate
	}
	return meta
}

func applyServerMessage(meta ServerMetadata, msg protoreflect.Message) ServerMetadata {
	fields := msg.Descriptor().Fields()
	if name := stringField(msg, fields.ByName("name")); name != "" {
		meta.Name = name
	}
	if title := stringField(msg, fields.ByName("title")); title != "" {
		meta.Title = title
	}
	if version := stringField(msg, fields.ByName("version")); version != "" {
		meta.Version = version
	}
	if instructions := stringField(msg, fields.ByName("instructions")); instructions != "" {
		meta.Instructions = instructions
	}
	if websiteURL := stringField(msg, fields.ByName("website_url")); websiteURL != "" {
		meta.WebsiteURL = websiteURL
	}
	return meta
}

func applyServiceMessage(meta ServerMetadata, msg protoreflect.Message) ServerMetadata {
	fields := msg.Descriptor().Fields()
	if toolPrefix := stringField(msg, fields.ByName("tool_prefix")); toolPrefix != "" {
		meta.ToolPrefix = toolPrefix
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

func serverFallback(service protoreflect.ServiceDescriptor) ServerMetadata {
	if service == nil {
		return ServerMetadata{}
	}
	return ServerMetadata{
		Name:    string(service.Name()),
		Version: "dev",
	}
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

func findExtension(file protoreflect.FileDescriptor, names []protoreflect.FullName) protoreflect.ExtensionDescriptor {
	seen := map[string]bool{}
	return findExtensionInFile(file, names, seen)
}

func findExtensionInFile(file protoreflect.FileDescriptor, names []protoreflect.FullName, seen map[string]bool) protoreflect.ExtensionDescriptor {
	if file == nil || seen[file.Path()] {
		return nil
	}
	seen[file.Path()] = true

	extensions := file.Extensions()
	for i := 0; i < extensions.Len(); i++ {
		ext := extensions.Get(i)
		for _, name := range names {
			if ext.FullName() == name {
				return ext
			}
		}
	}

	imports := file.Imports()
	for i := 0; i < imports.Len(); i++ {
		if ext := findExtensionInFile(imports.Get(i).FileDescriptor, names, seen); ext != nil {
			return ext
		}
	}
	return nil
}
