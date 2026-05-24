package discovery

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/grpc"
	reflectionv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

func FindService(files *protoregistry.Files, service string) (protoreflect.ServiceDescriptor, error) {
	if files == nil {
		return nil, fmt.Errorf("descriptor registry is nil")
	}
	desc, err := files.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return nil, fmt.Errorf("service %s: %w", service, err)
	}
	serviceDesc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s is a %T, not a service", service, desc)
	}
	return serviceDesc, nil
}

func LoadService(ctx context.Context, conn grpc.ClientConnInterface, service string) (protoreflect.ServiceDescriptor, error) {
	files, err := LoadFilesForSymbol(ctx, conn, service)
	if err != nil {
		return nil, err
	}
	return FindService(files, service)
}

func LoadServices(ctx context.Context, conn grpc.ClientConnInterface, services []string) ([]protoreflect.ServiceDescriptor, error) {
	names := normalizeServiceNames(services)
	var err error
	if len(names) == 0 {
		names, err = ListServiceNames(ctx, conn)
		if err != nil {
			return nil, err
		}
	}

	out := make([]protoreflect.ServiceDescriptor, 0, len(names))
	for _, name := range names {
		service, err := LoadService(ctx, conn, name)
		if err != nil {
			return nil, err
		}
		out = append(out, service)
	}
	return out, nil
}

func ListServiceNames(ctx context.Context, conn grpc.ClientConnInterface) ([]string, error) {
	client := reflectionv1alpha.NewServerReflectionClient(conn)
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("open grpc reflection stream: %w", err)
	}
	if err := stream.Send(&reflectionv1alpha.ServerReflectionRequest{
		MessageRequest: &reflectionv1alpha.ServerReflectionRequest_ListServices{
			ListServices: "*",
		},
	}); err != nil {
		return nil, fmt.Errorf("request service list: %w", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("receive service list: %w", err)
	}
	if refErr := resp.GetErrorResponse(); refErr != nil {
		return nil, fmt.Errorf("reflection error listing services: %s", refErr.ErrorMessage)
	}

	list := resp.GetListServicesResponse()
	if list == nil {
		return nil, fmt.Errorf("reflection response did not include service list")
	}
	names := make([]string, 0, len(list.GetService()))
	for _, service := range list.GetService() {
		name := service.GetName()
		if isReflectionService(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func LoadFilesForSymbol(ctx context.Context, conn grpc.ClientConnInterface, symbol string) (*protoregistry.Files, error) {
	client := reflectionv1alpha.NewServerReflectionClient(conn)
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("open grpc reflection stream: %w", err)
	}
	if err := stream.Send(&reflectionv1alpha.ServerReflectionRequest{
		MessageRequest: &reflectionv1alpha.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: symbol,
		},
	}); err != nil {
		return nil, fmt.Errorf("request descriptor for %s: %w", symbol, err)
	}
	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("receive descriptor for %s: %w", symbol, err)
	}
	if refErr := resp.GetErrorResponse(); refErr != nil {
		return nil, fmt.Errorf("reflection error for %s: %s", symbol, refErr.ErrorMessage)
	}

	set := &descriptorpb.FileDescriptorSet{}
	for _, raw := range resp.GetFileDescriptorResponse().GetFileDescriptorProto() {
		file := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(raw, file); err != nil {
			return nil, fmt.Errorf("decode file descriptor: %w", err)
		}
		set.File = append(set.File, file)
	}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, fmt.Errorf("build descriptor registry: %w", err)
	}
	return files, nil
}

func normalizeServiceNames(services []string) []string {
	out := make([]string, 0, len(services))
	seen := map[string]struct{}{}
	for _, service := range services {
		service = strings.TrimSpace(service)
		if service == "" {
			continue
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		out = append(out, service)
	}
	return out
}

func isReflectionService(name string) bool {
	return strings.HasPrefix(name, "grpc.reflection.")
}
