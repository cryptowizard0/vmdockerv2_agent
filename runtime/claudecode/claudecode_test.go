package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

func TestRuntimeApplyCreatesSessionAndReturnsReply(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "claude.log")
	workspace := t.TempDir()
	cliPath := writeFakeClaudeCLI(t, `#!/bin/sh
printf 'cwd=%s\n' "$PWD" >>"$FAKE_CLAUDE_LOG"
printf 'args=%s\n' "$*" >>"$FAKE_CLAUDE_LOG"
printf 'base=%s\n' "$ANTHROPIC_BASE_URL" >>"$FAKE_CLAUDE_LOG"
printf '{"type":"result","subtype":"success","is_error":false,"result":"hello from claude","session_id":"sess-1"}'
`)

	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", workspace)
	t.Setenv("ANTHROPIC_BASE_URL", "https://anthropic-proxy.example.com")
	t.Setenv("FAKE_CLAUDE_LOG", logPath)

	rt, err := NewWithParams(map[string]string{"model": "claude-sonnet-4-5"})
	if err != nil {
		t.Fatalf("NewWithParams failed: %v", err)
	}

	result, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Chat", Sequence: 8}, map[string]string{"Command": "hello"})
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if result.Data != "hello from claude" {
		t.Fatalf("result data = %q, want %q", result.Data, "hello from claude")
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Target != "target-1" {
		t.Fatalf("target = %q, want %q", result.Messages[0].Target, "target-1")
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake claude log failed: %v", err)
	}
	full := string(raw)
	expectedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("eval symlinks workspace failed: %v", err)
	}
	for _, item := range []string{
		"cwd=" + expectedWorkspace,
		"args=-p hello --output-format json --dangerously-skip-permissions --model claude-sonnet-4-5",
		"base=https://anthropic-proxy.example.com",
	} {
		if !strings.Contains(full, item) {
			t.Fatalf("expected %q in log:\n%s", item, full)
		}
	}
}

func TestRuntimeApplyResumeUsesCheckpointSessionID(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "claude.log")
	workspace := t.TempDir()
	cliPath := writeFakeClaudeCLI(t, `#!/bin/sh
printf 'args=%s\n' "$*" >>"$FAKE_CLAUDE_LOG"
printf '{"type":"result","subtype":"success","is_error":false,"result":"continued","session_id":"sess-restored"}'
`)

	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", workspace)
	t.Setenv("FAKE_CLAUDE_LOG", logPath)

	rt, err := NewRestored(`{"format":"claudecode.runtime.v1","sessionId":"sess-restored","cwd":"/ignored","model":"state-model","baseURL":"https://state.example.com"}`, nil)
	if err != nil {
		t.Fatalf("NewRestored failed: %v", err)
	}

	if _, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Execute"}, map[string]string{"Command": "continue"}); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake claude log failed: %v", err)
	}
	full := string(raw)
	if !strings.Contains(full, "--resume sess-restored") {
		t.Fatalf("expected resume flag in log:\n%s", full)
	}
	if !strings.Contains(full, "-p continue") {
		t.Fatalf("expected prompt after -p in log:\n%s", full)
	}
	if !strings.Contains(full, "--model state-model") {
		t.Fatalf("expected restored model in log:\n%s", full)
	}
}

func TestRuntimeApplyRetriesEmptySuccessfulResult(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "claude.log")
	countPath := filepath.Join(t.TempDir(), "count")
	workspace := t.TempDir()
	cliPath := writeFakeClaudeCLI(t, `#!/bin/sh
count=0
if [ -f "$FAKE_CLAUDE_COUNT" ]; then
  count="$(cat "$FAKE_CLAUDE_COUNT")"
fi
count=$((count + 1))
printf '%s' "$count" >"$FAKE_CLAUDE_COUNT"
printf 'attempt=%s args=%s\n' "$count" "$*" >>"$FAKE_CLAUDE_LOG"
if [ "$count" -eq 1 ]; then
  printf '{"type":"result","subtype":"success","is_error":false,"result":"","session_id":"sess-retry"}'
  exit 0
fi
printf '{"type":"result","subtype":"success","is_error":false,"result":"retried reply","session_id":"sess-retry"}'
`)

	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", workspace)
	t.Setenv("FAKE_CLAUDE_LOG", logPath)
	t.Setenv("FAKE_CLAUDE_COUNT", countPath)

	rt, err := NewRestored(`{"format":"claudecode.runtime.v1","sessionId":"sess-retry"}`, nil)
	if err != nil {
		t.Fatalf("NewRestored failed: %v", err)
	}

	result, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Chat"}, map[string]string{"Command": "retry me"})
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if result.Data != "retried reply" {
		t.Fatalf("result data = %q, want %q", result.Data, "retried reply")
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake claude log failed: %v", err)
	}
	full := string(raw)
	if strings.Count(full, "attempt=") != 2 {
		t.Fatalf("expected 2 attempts in log, got:\n%s", full)
	}
}

func TestRuntimeRestoreEnvOverridesCheckpointBaseURL(t *testing.T) {
	workspace := t.TempDir()
	cliPath := writeFakeClaudeCLI(t, `#!/bin/sh
printf '{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"sess-2"}'
`)

	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", workspace)
	t.Setenv("ANTHROPIC_BASE_URL", "https://env.example.com")

	rt, err := NewRestored(`{"format":"claudecode.runtime.v1","sessionId":"sess-2","baseURL":"https://checkpoint.example.com"}`, nil)
	if err != nil {
		t.Fatalf("NewRestored failed: %v", err)
	}

	state, err := rt.Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}

	var got checkpointState
	if err := json.Unmarshal([]byte(state), &got); err != nil {
		t.Fatalf("unmarshal checkpoint failed: %v", err)
	}
	if got.BaseURL != "https://env.example.com" {
		t.Fatalf("checkpoint baseURL = %q, want %q", got.BaseURL, "https://env.example.com")
	}
}

func TestRuntimeCheckpointAllowsEmptySession(t *testing.T) {
	workspace := t.TempDir()
	cliPath := writeFakeClaudeCLI(t, `#!/bin/sh
printf '{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"sess-1"}'
`)

	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", workspace)

	rt, err := NewWithParams(nil)
	if err != nil {
		t.Fatalf("NewWithParams failed: %v", err)
	}

	state, err := rt.Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}
	if !strings.Contains(state, `"format":"claudecode.runtime.v1"`) {
		t.Fatalf("unexpected checkpoint state: %s", state)
	}
}

func TestRuntimeApplyTimeout(t *testing.T) {
	workspace := t.TempDir()
	cliPath := writeFakeClaudeCLI(t, `#!/bin/sh
sleep 1
printf '{"type":"result","subtype":"success","is_error":false,"result":"late","session_id":"sess-late"}'
`)

	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", workspace)
	t.Setenv("CLAUDE_CODE_TIMEOUT_MS", "10")

	rt, err := NewWithParams(nil)
	if err != nil {
		t.Fatalf("NewWithParams failed: %v", err)
	}

	if _, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Query"}, map[string]string{"Command": "slow"}); err == nil || !strings.Contains(err.Error(), "timeout after") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestRuntimeApplyReturnsCLIError(t *testing.T) {
	workspace := t.TempDir()
	cliPath := writeFakeClaudeCLI(t, `#!/bin/sh
echo 'boom on stderr' >&2
exit 9
`)

	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", workspace)

	rt, err := NewWithParams(nil)
	if err != nil {
		t.Fatalf("NewWithParams failed: %v", err)
	}

	if _, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Query"}, map[string]string{"Command": "fail"}); err == nil || !strings.Contains(err.Error(), "boom on stderr") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

func TestRuntimeUsesDedicatedAgentWorkspaceOverRuntimeRoot(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "claude.log")
	runtimeRoot := t.TempDir()
	agentWorkspace := filepath.Join(runtimeRoot, "workspace")
	if err := os.MkdirAll(agentWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir agent workspace failed: %v", err)
	}
	cliPath := writeFakeClaudeCLI(t, `#!/bin/sh
printf 'cwd=%s\n' "$PWD" >>"$FAKE_CLAUDE_LOG"
printf '{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"sess-3"}'
`)

	t.Setenv("CLAUDE_CODE_BIN", cliPath)
	t.Setenv("VMDOCKER_RUNTIME_WORKSPACE", runtimeRoot)
	t.Setenv("VMDOCKER_AGENT_WORKSPACE", agentWorkspace)
	t.Setenv("FAKE_CLAUDE_LOG", logPath)

	rt, err := NewWithParams(nil)
	if err != nil {
		t.Fatalf("NewWithParams failed: %v", err)
	}
	if _, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Query"}, map[string]string{"Command": "pwd"}); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake claude log failed: %v", err)
	}
	expectedAgentWorkspace, err := filepath.EvalSymlinks(agentWorkspace)
	if err != nil {
		t.Fatalf("eval symlinks agent workspace failed: %v", err)
	}
	if !strings.Contains(string(raw), "cwd="+expectedAgentWorkspace) {
		t.Fatalf("expected dedicated agent workspace in log, got %s", string(raw))
	}
}

func TestParseFlagsSupportsQuotedValues(t *testing.T) {
	flags, err := parseFlags(`--append-system-prompt "foo bar" --permission-prompt-tool 'tool name'`)
	if err != nil {
		t.Fatalf("parseFlags failed: %v", err)
	}
	want := []string{"--append-system-prompt", "foo bar", "--permission-prompt-tool", "tool name"}
	if strings.Join(flags, "\n") != strings.Join(want, "\n") {
		t.Fatalf("flags = %v, want %v", flags, want)
	}
}

func TestParseFlagsRejectsUnterminatedQuote(t *testing.T) {
	if _, err := parseFlags(`--append-system-prompt "foo`); err == nil {
		t.Fatalf("expected unterminated quote error")
	}
}

func writeFakeClaudeCLI(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude failed: %v", err)
	}
	return path
}
