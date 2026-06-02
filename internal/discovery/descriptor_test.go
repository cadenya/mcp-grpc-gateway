package discovery_test

import (
	"context"
	"os"
	"testing"

	"go.cadenya.com/mcp-grpc-gateway/internal/discovery"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestServicesFromDescriptorSetReturnsAllServices(t *testing.T) {
	set := testDescriptorSet()

	got, err := discovery.ServicesFromDescriptorSet(set, nil)

	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "test.v1.AlphaService", string(got[0].FullName()))
	require.Equal(t, "test.v1.BetaService", string(got[1].FullName()))
}

func TestServicesFromDescriptorSetFiltersToRequestedServices(t *testing.T) {
	set := testDescriptorSet()

	got, err := discovery.ServicesFromDescriptorSet(set, []string{"test.v1.BetaService"})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "test.v1.BetaService", string(got[0].FullName()))
}

func TestServicesFromDescriptorSetReturnsErrorForMissingService(t *testing.T) {
	set := testDescriptorSet()

	_, err := discovery.ServicesFromDescriptorSet(set, []string{"test.v1.Missing"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "test.v1.Missing")
}

func TestServicesFromDescriptorSetReturnsErrorWhenEmpty(t *testing.T) {
	set := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name:    ptr("empty.proto"),
				Package: ptr("empty"),
				Syntax:  ptr("proto3"),
			},
		},
	}

	_, err := discovery.ServicesFromDescriptorSet(set, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "no services found")
}

func TestLoadServicesFromFileReadsAndParsesDescriptorSet(t *testing.T) {
	path := writeDescriptorSetFile(t, testDescriptorSet())

	loader := discovery.LoadServicesFromFile(path)
	got, err := loader(context.Background(), nil, nil)

	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestLoadServicesFromFileReturnsErrorForMissingFile(t *testing.T) {
	loader := discovery.LoadServicesFromFile("/nonexistent/path.pb")

	_, err := loader(context.Background(), nil, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "read proto descriptor file")
}

func TestLoadServicesFromFileReturnsErrorForInvalidFile(t *testing.T) {
	path := t.TempDir() + "/bad.pb"
	require.NoError(t, os.WriteFile(path, []byte("not a protobuf"), 0o600))

	loader := discovery.LoadServicesFromFile(path)
	_, err := loader(context.Background(), nil, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal proto descriptor file")
}

func testDescriptorSet() *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name:    ptr("test/v1/services.proto"),
				Package: ptr("test.v1"),
				Syntax:  ptr("proto3"),
				MessageType: []*descriptorpb.DescriptorProto{
					{Name: ptr("AlphaRequest")},
					{Name: ptr("AlphaResponse")},
					{Name: ptr("BetaRequest")},
					{Name: ptr("BetaResponse")},
				},
				Service: []*descriptorpb.ServiceDescriptorProto{
					{
						Name: ptr("AlphaService"),
						Method: []*descriptorpb.MethodDescriptorProto{{
							Name:       ptr("DoAlpha"),
							InputType:  ptr(".test.v1.AlphaRequest"),
							OutputType: ptr(".test.v1.AlphaResponse"),
						}},
					},
					{
						Name: ptr("BetaService"),
						Method: []*descriptorpb.MethodDescriptorProto{{
							Name:       ptr("DoBeta"),
							InputType:  ptr(".test.v1.BetaRequest"),
							OutputType: ptr(".test.v1.BetaResponse"),
						}},
					},
				},
			},
		},
	}
}

func writeDescriptorSetFile(t *testing.T, set *descriptorpb.FileDescriptorSet) string {
	t.Helper()
	raw, err := proto.Marshal(set)
	require.NoError(t, err)
	path := t.TempDir() + "/test.pb"
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	return path
}
