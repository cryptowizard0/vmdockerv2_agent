package hermes

import (
	"strings"
	"testing"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

func TestRuntimeApplyIsUnsupported(t *testing.T) {
	rt, err := NewWithParams(nil)
	if err != nil {
		t.Fatalf("NewWithParams failed: %v", err)
	}
	_, err = rt.Apply("target-1", vmmSchema.Meta{Action: "Chat"}, map[string]string{"Command": "hello"})
	if err == nil || !strings.Contains(err.Error(), "does not support Apply") {
		t.Fatalf("expected unsupported Apply error, got %v", err)
	}
}

func TestRuntimeCheckpointCanBeRestored(t *testing.T) {
	rt, err := NewWithParams(nil)
	if err != nil {
		t.Fatalf("NewWithParams failed: %v", err)
	}
	checkpoint, err := rt.Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}
	if checkpoint != `{"format":"hermes.gateway.runtime.v2"}` {
		t.Fatalf("checkpoint = %s", checkpoint)
	}
	if _, err := NewRestored(checkpoint, nil); err != nil {
		t.Fatalf("NewRestored failed: %v", err)
	}
}

func TestRuntimeRestoreRejectsUnknownFormat(t *testing.T) {
	if _, err := NewRestored(`{"format":"unknown"}`, nil); err == nil || !strings.Contains(err.Error(), "unsupported hermes checkpoint format") {
		t.Fatalf("expected unsupported format error, got %v", err)
	}
}
