package auth

import "testing"

func TestToolPrefixDisabled(t *testing.T) {
	var a *Auth
	if a.ToolPrefixDisabled() {
		t.Error("nil auth should return false")
	}

	a = &Auth{}
	if a.ToolPrefixDisabled() {
		t.Error("empty auth should return false")
	}

	a = &Auth{Metadata: map[string]any{"tool_prefix_disabled": true}}
	if !a.ToolPrefixDisabled() {
		t.Error("should return true when set to true")
	}

	a = &Auth{Metadata: map[string]any{"tool_prefix_disabled": "true"}}
	if !a.ToolPrefixDisabled() {
		t.Error("should return true when set to string 'true'")
	}

	a = &Auth{Metadata: map[string]any{"tool-prefix-disabled": true}}
	if !a.ToolPrefixDisabled() {
		t.Error("should return true with kebab-case key")
	}

	a = &Auth{Metadata: map[string]any{"tool_prefix_disabled": false}}
	if a.ToolPrefixDisabled() {
		t.Error("should return false when set to false")
	}
}

func TestWebsocketIncrementalEnabled(t *testing.T) {
	tests := []struct {
		name       string
		attributes map[string]string
		metadata   map[string]any
		want       bool
	}{
		{
			name: "default false",
			want: false,
		},
		{
			name:       "attributes true",
			attributes: map[string]string{"websockets": "true"},
			want:       true,
		},
		{
			name:       "attributes false",
			attributes: map[string]string{"websockets": "false"},
			want:       false,
		},
		{
			name:       "attributes parsebool true alias",
			attributes: map[string]string{"websockets": "1"},
			want:       true,
		},
		{
			name:       "attributes parsebool false alias",
			attributes: map[string]string{"websockets": "0"},
			want:       false,
		},
		{
			name:       "attributes uppercase true with spaces",
			attributes: map[string]string{"websockets": " TRUE "},
			want:       true,
		},
		{
			name:     "metadata bool true",
			metadata: map[string]any{"websockets": true},
			want:     true,
		},
		{
			name:     "metadata bool false",
			metadata: map[string]any{"websockets": false},
			want:     false,
		},
		{
			name:     "metadata string true",
			metadata: map[string]any{"websockets": " true "},
			want:     true,
		},
		{
			name:     "metadata string false",
			metadata: map[string]any{"websockets": "FALSE"},
			want:     false,
		},
		{
			name:     "metadata invalid string",
			metadata: map[string]any{"websockets": "nope"},
			want:     false,
		},
		{
			name:     "metadata unsupported type",
			metadata: map[string]any{"websockets": 1},
			want:     false,
		},
		{
			name:     "metadata nil value",
			metadata: map[string]any{"websockets": nil},
			want:     false,
		},
		{
			name:       "attributes precedence over metadata",
			attributes: map[string]string{"websockets": "false"},
			metadata:   map[string]any{"websockets": true},
			want:       false,
		},
		{
			name:       "invalid attributes fallback to metadata",
			attributes: map[string]string{"websockets": "invalid"},
			metadata:   map[string]any{"websockets": true},
			want:       true,
		},
		{
			name:       "empty attributes fallback to metadata",
			attributes: map[string]string{"websockets": "  "},
			metadata:   map[string]any{"websockets": true},
			want:       true,
		},
		{
			name:       "missing attribute key fallback to metadata",
			attributes: map[string]string{"other": "x"},
			metadata:   map[string]any{"websockets": true},
			want:       true,
		},
		{
			name:       "invalid attributes and invalid metadata",
			attributes: map[string]string{"websockets": "invalid"},
			metadata:   map[string]any{"websockets": "invalid"},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WebsocketIncrementalEnabled(tt.attributes, tt.metadata); got != tt.want {
				t.Fatalf("WebsocketIncrementalEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
