package hermes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

func TestRuntimeApplySendsPromptToGatewaySession(t *testing.T) {
	var gotSessionIDs []string
	var gotPrompts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer gateway-secret" {
			t.Fatalf("authorization = %q", got)
		}
		gotSessionIDs = append(gotSessionIDs, r.Header.Get("X-Hermes-Session-Id"))
		var body chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "custom-hermes" || len(body.Messages) != 1 {
			t.Fatalf("unexpected request: %#v", body)
		}
		gotPrompts = append(gotPrompts, body.Messages[0].Content)
		w.Header().Set("X-Hermes-Session-Id", "gateway-session")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello from gateway"}}]}`))
	}))
	defer server.Close()

	t.Setenv("HERMES_GATEWAY_URL", server.URL)
	t.Setenv("HERMES_GATEWAY_TOKEN", "gateway-secret")
	t.Setenv("HERMES_GATEWAY_MODEL", "custom-hermes")
	rt, err := NewWithParams(nil)
	if err != nil {
		t.Fatalf("NewWithParams failed: %v", err)
	}
	result, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Chat", Sequence: 8}, map[string]string{"Command": "hello"})
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if result.Data != "hello from gateway" {
		t.Fatalf("result data = %q", result.Data)
	}
	if len(gotSessionIDs) != 1 || gotSessionIDs[0] == "" {
		t.Fatal("gateway session ID was not sent")
	}
	if _, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Chat"}, map[string]string{"Command": "again"}); err != nil {
		t.Fatalf("second Apply failed: %v", err)
	}
	if len(gotSessionIDs) != 2 || gotSessionIDs[1] != "gateway-session" {
		t.Fatalf("session IDs = %v, second request should reuse gateway session", gotSessionIDs)
	}
	if strings.Join(gotPrompts, ",") != "hello,again" {
		t.Fatalf("prompts = %v", gotPrompts)
	}
	checkpoint, err := rt.Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}
	if !strings.Contains(checkpoint, `"sessionId":"gateway-session"`) {
		t.Fatalf("checkpoint did not use gateway session: %s", checkpoint)
	}
}

func TestRuntimeRestoreReusesGatewaySession(t *testing.T) {
	var gotSessionID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSessionID = r.Header.Get("X-Hermes-Session-Id")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"restored"}}]}`))
	}))
	defer server.Close()

	t.Setenv("HERMES_GATEWAY_URL", server.URL)
	rt, err := NewRestored(`{"format":"hermes.gateway.runtime.v1","sessionId":"session-restored"}`, nil)
	if err != nil {
		t.Fatalf("NewRestored failed: %v", err)
	}
	if _, err := rt.Apply("target-1", vmmSchema.Meta{}, map[string]string{"Command": "continue"}); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if gotSessionID != "session-restored" {
		t.Fatalf("session ID = %q", gotSessionID)
	}
	checkpoint, err := rt.Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}
	if !strings.Contains(checkpoint, `"sessionId":"session-restored"`) {
		t.Fatalf("checkpoint = %s", checkpoint)
	}
}

func TestRuntimeApplyReportsGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"model unavailable"}}`, http.StatusBadGateway)
	}))
	defer server.Close()
	t.Setenv("HERMES_GATEWAY_URL", server.URL)

	rt, err := NewWithParams(nil)
	if err != nil {
		t.Fatalf("NewWithParams failed: %v", err)
	}
	_, err = rt.Apply("target-1", vmmSchema.Meta{}, map[string]string{"Command": "hello"})
	if err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("expected gateway error, got %v", err)
	}
}
