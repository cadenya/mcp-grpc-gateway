package discovery

import (
	"context"
	"fmt"
	"os"
	"sort"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

type Loader = func(context.Context, grpc.ClientConnInterface, []string) ([]protoreflect.ServiceDescriptor, error)

func LoadServicesFromFile(path string) Loader {
	return func(_ context.Context, _ grpc.ClientConnInterface, services []string) ([]protoreflect.ServiceDescriptor, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read proto descriptor file: %w", err)
		}

		set := &descriptorpb.FileDescriptorSet{}
		if err := proto.Unmarshal(raw, set); err != nil {
			return nil, fmt.Errorf("unmarshal proto descriptor file: %w", err)
		}

		return ServicesFromDescriptorSet(set, services)
	}
}

func ServicesFromDescriptorSet(set *descriptorpb.FileDescriptorSet, services []string) ([]protoreflect.ServiceDescriptor, error) {
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, fmt.Errorf("build descriptor registry: %w", err)
	}

	filter := normalizeServiceNames(services)

	if len(filter) > 0 {
		out := make([]protoreflect.ServiceDescriptor, 0, len(filter))
		for _, name := range filter {
			svc, err := FindService(files, name)
			if err != nil {
				return nil, err
			}
			out = append(out, svc)
		}
		return out, nil
	}

	var out []protoreflect.ServiceDescriptor
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			if isReflectionService(string(svc.FullName())) {
				continue
			}
			out = append(out, svc)
		}
		return true
	})

	sort.Slice(out, func(i, j int) bool {
		return out[i].FullName() < out[j].FullName()
	})

	if len(out) == 0 {
		return nil, fmt.Errorf("no services found in descriptor set")
	}
	return out, nil
}
