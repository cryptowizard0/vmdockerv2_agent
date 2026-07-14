package runtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	schema "github.com/cryptowizard0/vmdockerv2_agent/runtime/openclaw/schema"
	"github.com/cryptowizard0/vmdockerv2_agent/runtime/telegramcustomer"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

func TestNewRuntimeOpenclaw(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok","data":"pong"}`))
			return
		}
		if r.URL.Path == "/tools/invoke" {
			var req schema.ToolInvokeRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Tool == "sessions_create" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok","data":{"sessionId":"runtime-sess-1"}}`))
				return
			}
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error"}`))
	}))
	defer gateway.Close()

	t.Setenv("RUNTIME_TYPE", RuntimeTypeOpenclaw)
	t.Setenv("OPENCLAW_GATEWAY_URL", gateway.URL)
	t.Setenv("OPENCLAW_TIMEOUT_MS", "1000")

	rt, err := New(vmmSchema.Env{}, "", "", nil, nil)
	if err != nil {
		t.Fatalf("new runtime failed: %v", err)
	}
	if rt == nil || rt.vm == nil {
		t.Fatalf("runtime vm is nil")
	}
}

func TestNewRestoredRuntimeOpenclaw(t *testing.T) {
	createCalled := false
	sendSessionKey := ""

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok","data":"pong"}`))
			return
		}
		if r.URL.Path == "/tools/invoke" {
			var req schema.ToolInvokeRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			switch req.Tool {
			case "sessions_create":
				createCalled = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok","data":{"sessionId":"unexpected"}}`))
				return
			case "sessions_send":
				sendSessionKey = req.SessionKey
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok","data":"restored"}`))
				return
			}
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error"}`))
	}))
	defer gateway.Close()

	t.Setenv("RUNTIME_TYPE", RuntimeTypeOpenclaw)
	t.Setenv("OPENCLAW_GATEWAY_URL", gateway.URL)
	t.Setenv("OPENCLAW_TIMEOUT_MS", "1000")

	rt, err := NewRestored(vmmSchema.Env{}, "", "", nil, `{"format":"openclaw.runtime.v1","sessionId":"runtime-restored-1"}`)
	if err != nil {
		t.Fatalf("new restored runtime failed: %v", err)
	}
	if rt == nil || rt.vm == nil {
		t.Fatalf("runtime vm is nil")
	}

	if _, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Execute"}, map[string]string{"Command": "hi"}); err != nil {
		t.Fatalf("apply after runtime restore failed: %v", err)
	}
	if createCalled {
		t.Fatalf("did not expect sessions_create in restore path")
	}
	if sendSessionKey != "runtime-restored-1" {
		t.Fatalf("expected restored session key runtime-restored-1, got %q", sendSessionKey)
	}
}

func TestNewRuntimeClaude(t *testing.T) {
	workspace := t.TempDir()
	cliPath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nprintf '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"ok\",\"session_id\":\"claude-session-1\"}'\n"
	if err := os.WriteFile(cliPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude failed: %v", err)
	}

	t.Setenv("RUNTIME_TYPE", RuntimeTypeClaude)
	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", workspace)
	t.Setenv("ANTHROPIC_MODEL", "test-model")

	rt, err := New(vmmSchema.Env{}, "", "", nil, map[string]string{"model": "qwen3.5-plus"})
	if err != nil {
		t.Fatalf("new runtime failed: %v", err)
	}
	if rt == nil || rt.vm == nil {
		t.Fatalf("runtime vm is nil")
	}
}

func TestNewRestoredRuntimeClaude(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "claude.log")
	workspace := t.TempDir()
	cliPath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nprintf '%s\n' \"$*\" >>" + shellQuoteForTest(logPath) + "\nprintf '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"restored\",\"session_id\":\"runtime-restored-1\"}'\n"
	if err := os.WriteFile(cliPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude failed: %v", err)
	}

	t.Setenv("RUNTIME_TYPE", RuntimeTypeClaude)
	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", workspace)

	rt, err := NewRestored(vmmSchema.Env{}, "", "", nil, `{"format":"claudecode.runtime.v1","sessionId":"runtime-restored-1"}`)
	if err != nil {
		t.Fatalf("new restored runtime failed: %v", err)
	}
	if rt == nil || rt.vm == nil {
		t.Fatalf("runtime vm is nil")
	}

	if _, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Execute"}, map[string]string{"Command": "hi"}); err != nil {
		t.Fatalf("apply after runtime restore failed: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake claude log failed: %v", err)
	}
	if !strings.Contains(string(raw), "--resume runtime-restored-1") {
		t.Fatalf("expected restored session id in log, got %s", string(raw))
	}
}

func TestNewRuntimeTelegramCustomer(t *testing.T) {
	workspace := t.TempDir()
	homeDir := t.TempDir()
	cliPath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(cliPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude failed: %v", err)
	}

	originalRunByHymx := telegramcustomer.RunByHymx
	telegramcustomer.RunByHymx = func(workspaceDir, runtimeHomeDir, botToken string) error {
		if workspaceDir != workspace {
			t.Fatalf("expected workspace %q, got %q", workspace, workspaceDir)
		}
		if runtimeHomeDir != homeDir {
			t.Fatalf("expected runtime home %q, got %q", homeDir, runtimeHomeDir)
		}
		if botToken != "bot-token" {
			t.Fatalf("expected bot token to be forwarded")
		}
		return nil
	}
	defer func() {
		telegramcustomer.RunByHymx = originalRunByHymx
	}()

	t.Setenv("RUNTIME_TYPE", RuntimeTypeTelegramCustomer)
	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", workspace)
	t.Setenv("VMDOCKER_RUNTIME_HOME", homeDir)
	t.Setenv("BOT_TOKEN", "bot-token")

	rt, err := New(vmmSchema.Env{}, "", "", nil, map[string]string{"model": "qwen3.5-plus"})
	if err != nil {
		t.Fatalf("new telegramcustomer runtime failed: %v", err)
	}
	if rt == nil || rt.vm == nil {
		t.Fatalf("runtime vm is nil")
	}

	state, err := rt.Checkpoint()
	if err != nil {
		t.Fatalf("checkpoint failed: %v", err)
	}
	if !strings.Contains(state, "telegramcustomer.runtime.v1") {
		t.Fatalf("expected telegramcustomer checkpoint format, got %s", state)
	}
}

func shellQuoteForTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
