package openclaw

import (
	"testing"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

func TestExtractModelName(t *testing.T) {
	tests := []struct {
		name   string
		meta   vmmSchema.Meta
		params map[string]string
		want   string
	}{
		{
			name: "provider prefixes plain model",
			params: map[string]string{
				"provider": "zen",
				"model":    "plan",
			},
			want: "zen/plan",
		},
		{
			name: "provider rewrites prefixed model",
			params: map[string]string{
				"provider": "zen",
				"model":    "kimi-coding/plan",
			},
			want: "zen/plan",
		},
		{
			name: "model without provider stays unchanged",
			params: map[string]string{
				"model": "kimi-coding/k2p5",
			},
			want: "kimi-coding/k2p5",
		},
		{
			name: "meta data follows provider rewrite",
			meta: vmmSchema.Meta{
				Data: "kimi-coding/plan",
			},
			params: map[string]string{
				"provider": "zen",
			},
			want: "zen/plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractModelName(tt.meta, tt.params)
			if got != tt.want {
				t.Fatalf("extractModelName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractProviderName(t *testing.T) {
	tests := []struct {
		name   string
		meta   vmmSchema.Meta
		params map[string]string
		want   string
	}{
		{
			name: "provider tag wins",
			params: map[string]string{
				"provider": "Zen",
				"model":    "kimi-coding/plan",
			},
			want: "zen",
		},
		{
			name: "provider falls back to model prefix",
			params: map[string]string{
				"model": "kimi-coding/k2p5",
			},
			want: "kimi-coding",
		},
		{
			name: "empty when neither provider nor prefixed model exists",
			params: map[string]string{
				"model": "plan",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractProviderName(tt.meta, tt.params)
			if got != tt.want {
				t.Fatalf("extractProviderName() = %q, want %q", got, tt.want)
			}
		})
	}
}
