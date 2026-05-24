package schema_test

import (
	"testing"

	gatewayschema "cadenya.com/mcp-grpc-gateway/internal/schema"
	"cadenya.com/mcp-grpc-gateway/internal/testpb"
	"github.com/stretchr/testify/require"
)

func TestComplexProtoTypesConvertToJSONSchema(t *testing.T) {
	msg := testpb.File_functional_v1_complex_proto.Messages().ByName("ComplexRequest")

	got, err := gatewayschema.ForMessage(msg)

	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"priority": prioritySchema(),
			"metadata": metadataSchema(),
			"targets": map[string]any{
				"type":  "array",
				"items": targetSchema(),
			},
			"targetByKey": map[string]any{
				"type":                 "object",
				"additionalProperties": targetSchema(),
			},
			"metadataByKey": map[string]any{
				"type":                 "object",
				"additionalProperties": metadataSchema(),
			},
			"priorities": map[string]any{
				"type":  "array",
				"items": prioritySchema(),
			},
		},
	}, got)
}

func prioritySchema() map[string]any {
	return map[string]any{
		"type": "string",
		"enum": []any{"PRIORITY_UNSPECIFIED", "PRIORITY_LOW", "PRIORITY_HIGH"},
	}
}

func metadataSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source": map[string]any{"type": "string"},
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
	}
}

func targetSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":       map[string]any{"type": "string"},
			"metadata": metadataSchema(),
		},
	}
}
