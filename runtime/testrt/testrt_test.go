package testrt

import (
	"testing"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

func TestRuntimeTestApply(t *testing.T) {
	rt, err := NewRuntimeTest()
	if err != nil {
		t.Fatalf("create test runtime: %v", err)
	}

	t.Run("ping action returns pong", func(t *testing.T) {
		res, err := rt.Apply("target-1", vmmSchema.Meta{
			Action:   TestRuntimeActionPing,
			Sequence: 7,
		}, nil)
		if err != nil {
			t.Fatalf("apply failed: %v", err)
		}

		if res.Data != "Pong" {
			t.Fatalf("expected Pong, got %s", res.Data)
		}
		if len(res.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(res.Messages))
		}
		if res.Messages[0].Target != "target-1" {
			t.Fatalf("expected target-1, got %s", res.Messages[0].Target)
		}
		if res.Messages[0].Sequence != "7" {
			t.Fatalf("expected sequence 7, got %s", res.Messages[0].Sequence)
		}
	})

	t.Run("echo action uses input data", func(t *testing.T) {
		res, err := rt.Apply("", vmmSchema.Meta{
			Action: TestRuntimeActionEcho,
			Data:   "from-meta",
		}, map[string]string{
			"From": "fallback-target",
			"Data": "from-params",
		})
		if err != nil {
			t.Fatalf("apply failed: %v", err)
		}

		if res.Data != "from-params" {
			t.Fatalf("expected from-params, got %s", res.Data)
		}
		if res.Messages[0].Target != "fallback-target" {
			t.Fatalf("expected fallback-target, got %s", res.Messages[0].Target)
		}
	})
}
