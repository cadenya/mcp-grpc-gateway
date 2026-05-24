package annotations_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	grpcmcpgatewayv1 "cadenya.com/mcp-grpc-gateway/gen/grpcmcpgateway/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestAnnotationsProtoBuildsWithBuf(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skip("buf is not installed")
	}

	out := filepath.Join(t.TempDir(), "descriptor.binpb")
	cmd := exec.Command("buf", "build", ".", "--as-file-descriptor-set", "-o", out)
	cmd.Dir = "../.."
	rawOutput, err := cmd.CombinedOutput()
	require.NoError(t, err, string(rawOutput))

	rawDescriptor, err := os.ReadFile(out)
	require.NoError(t, err)
	set := &descriptorpb.FileDescriptorSet{}
	require.NoError(t, proto.Unmarshal(rawDescriptor, set))

	files, err := protodesc.NewFiles(set)
	require.NoError(t, err)
	desc, err := files.FindDescriptorByName("grpcmcpgateway.v1.Server")
	require.NoError(t, err)
	_, ok := desc.(protoreflect.MessageDescriptor)
	require.True(t, ok)

	desc, err = files.FindDescriptorByName("grpcmcpgateway.v1.Tool")
	require.NoError(t, err)
	_, ok = desc.(protoreflect.MessageDescriptor)
	require.True(t, ok)

	ext, err := files.FindDescriptorByName("grpcmcpgateway.v1.server")
	require.NoError(t, err)
	serverExt, ok := ext.(protoreflect.ExtensionDescriptor)
	require.True(t, ok)
	require.Equal(t, protoreflect.FieldNumber(grpcmcpgatewayv1.ServerExtensionNumber), serverExt.Number())

	ext, err = files.FindDescriptorByName("grpcmcpgateway.v1.tool")
	require.NoError(t, err)
	toolExt, ok := ext.(protoreflect.ExtensionDescriptor)
	require.True(t, ok)
	require.Equal(t, protoreflect.FieldNumber(grpcmcpgatewayv1.ToolExtensionNumber), toolExt.Number())

	ext, err = files.FindDescriptorByName("grpcmcpgateway.v1.field")
	require.NoError(t, err)
	fieldExt, ok := ext.(protoreflect.ExtensionDescriptor)
	require.True(t, ok)
	require.Equal(t, protoreflect.FieldNumber(grpcmcpgatewayv1.FieldExtensionNumber), fieldExt.Number())
}
