package annotations_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
	desc, err := files.FindDescriptorByName("grpcmcpgateway.v1.ToolOptions")
	require.NoError(t, err)
	_, ok := desc.(protoreflect.MessageDescriptor)
	require.True(t, ok)

	ext, err := files.FindDescriptorByName("grpcmcpgateway.v1.tool")
	require.NoError(t, err)
	_, ok = ext.(protoreflect.ExtensionDescriptor)
	require.True(t, ok)
}
