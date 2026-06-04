package toolcache

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffTools(t *testing.T) {
	tests := []struct {
		name              string
		previous          map[string]string
		current           map[string]string
		expectedAdded     []string
		expectedRemoved   []string
		expectedUnchanged []string
	}{
		{
			name:              "cold start treats every tool as added",
			previous:          nil,
			current:           map[string]string{"greet_user": "functional.v1.GreeterService"},
			expectedAdded:     []string{"greet_user"},
			expectedRemoved:   nil,
			expectedUnchanged: nil,
		},
		{
			name:              "identical reload reports everything unchanged",
			previous:          map[string]string{"greet_user": "functional.v1.GreeterService"},
			current:           map[string]string{"greet_user": "functional.v1.GreeterService"},
			expectedAdded:     nil,
			expectedRemoved:   nil,
			expectedUnchanged: []string{"greet_user"},
		},
		{
			name: "mixed reload sorts added, removed and unchanged",
			previous: map[string]string{
				"greet_user":  "functional.v1.GreeterService",
				"legacy_user": "functional.v1.GreeterService",
				"retire_user": "functional.v1.GreeterService",
			},
			current: map[string]string{
				"greet_user":    "functional.v1.GreeterService",
				"welcome_user":  "functional.v1.GreeterService",
				"farewell_user": "functional.v1.GreeterService",
			},
			expectedAdded:     []string{"farewell_user", "welcome_user"},
			expectedRemoved:   []string{"legacy_user", "retire_user"},
			expectedUnchanged: []string{"greet_user"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, removed, unchanged := diffTools(tt.previous, tt.current)
			require.Equal(t, tt.expectedAdded, added)
			require.Equal(t, tt.expectedRemoved, removed)
			require.Equal(t, tt.expectedUnchanged, unchanged)
		})
	}
}
