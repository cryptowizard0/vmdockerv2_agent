package hermes

import (
	"reflect"
	"strings"
	"testing"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

type fakeStarter struct {
	env        map[string]string
	program    string
	startCalls int
}

func (s *fakeStarter) Start(program string, env map[string]string) error {
	s.startCalls++
	s.program = program
	s.env = env
	return nil
}

func TestRuntimeApplyIgnoresUnsupportedAction(t *testing.T) {
	rt := &Runtime{owner: "target-1", starter: &fakeStarter{}}
	result, err := rt.Apply("target-1", vmmSchema.Meta{Action: "Chat"}, map[string]string{"Command": "hello"})
	if err != nil || result.Data != "" {
		t.Fatalf("unsupported action result = %+v, err = %v", result, err)
	}
}

func TestRuntimeApplyStartAgentUsesContainerEnvParams(t *testing.T) {
	starter := &fakeStarter{}
	rt := &Runtime{owner: "target-1", starter: starter}
	result, err := rt.Apply("target-1", vmmSchema.Meta{Action: startAgentAction}, map[string]string{
		"Container-Env-HERMES_AGENT_LLM_API_KEY": "secret",
		"Container-Env-HUB_GATEWAY_URL":          "https://hub.example",
		"Command":                                "must-not-be-an-env-var",
	})
	if err != nil {
		t.Fatalf("Apply Start-Agent failed: %v", err)
	}
	want := map[string]string{
		"HERMES_AGENT_LLM_API_KEY": "secret",
		"HUB_GATEWAY_URL":          "https://hub.example",
	}
	if !reflect.DeepEqual(starter.env, want) {
		t.Fatalf("start env = %#v, want %#v", starter.env, want)
	}
	if starter.program != defaultStartProgram {
		t.Fatalf("program = %q, want %q", starter.program, defaultStartProgram)
	}
	if result.Data != `{"status":"started"}` {
		t.Fatalf("result data = %q", result.Data)
	}
}

func TestRuntimeApplyStartAgentUsesSpecifiedProgram(t *testing.T) {
	starter := &fakeStarter{}
	rt := &Runtime{owner: "target-1", starter: starter}
	if _, err := rt.Apply("target-1", vmmSchema.Meta{Action: startAgentAction}, map[string]string{
		agentProgramParam: "my-agent",
	}); err != nil {
		t.Fatalf("Apply Start-Agent failed: %v", err)
	}
	if starter.program != "my-agent" {
		t.Fatalf("program = %q, want my-agent", starter.program)
	}
}

func TestRuntimeApplyStartAgentRejectsProgramPath(t *testing.T) {
	starter := &fakeStarter{}
	rt := &Runtime{owner: "target-1", starter: starter}
	if _, err := rt.Apply("target-1", vmmSchema.Meta{Action: startAgentAction}, map[string]string{
		agentProgramParam: "../my-agent",
	}); err == nil || !strings.Contains(err.Error(), "must be a program name") {
		t.Fatalf("expected program name error, got %v", err)
	}
	if starter.startCalls != 0 {
		t.Fatalf("starter called %d times for invalid program", starter.startCalls)
	}
}

func TestRuntimeApplyStartAgentIsIdempotent(t *testing.T) {
	starter := &fakeStarter{}
	rt := &Runtime{owner: "target-1", starter: starter}
	meta := vmmSchema.Meta{Action: startAgentAction}
	if _, err := rt.Apply("target-1", meta, nil); err != nil {
		t.Fatalf("first Apply failed: %v", err)
	}
	result, err := rt.Apply("target-1", meta, nil)
	if err != nil {
		t.Fatalf("second Apply failed: %v", err)
	}
	if starter.startCalls != 1 {
		t.Fatalf("start calls = %d, want 1", starter.startCalls)
	}
	if result.Data != `{"status":"already_started"}` {
		t.Fatalf("result data = %q", result.Data)
	}
}

func TestRuntimeApplyEvalStartsCommand(t *testing.T) {
	rt := &Runtime{owner: "target-1", starter: &fakeStarter{}}
	result, err := rt.Apply("target-1", vmmSchema.Meta{
		Action: evalAction,
		Data:   `ZXhpdCAw`,
	}, map[string]string{"Data": `exit 0`})
	if err != nil {
		t.Fatalf("Apply Eval failed: %v", err)
	}
	if result.Data != `{"status":"started"}` {
		t.Fatalf("Eval result = %q", result.Data)
	}
}

func TestRuntimeApplyEvalRejectsEmptyCommand(t *testing.T) {
	rt := &Runtime{owner: "target-1", starter: &fakeStarter{}}
	if _, err := rt.Apply("target-1", vmmSchema.Meta{Action: evalAction, Data: "ICA"}, nil); err == nil {
		t.Fatal("expected empty Eval command error")
	}
}

func TestEvalCommandUsesDecodedMetaDataForPlaintextEval(t *testing.T) {
	meta := vmmSchema.Meta{Data: "ZXhpdCAw"}
	got, err := evalCommand(meta, map[string]string{"Data": "reserved-field-is-ignored"})
	if err != nil || got != "exit 0" {
		t.Fatalf("Eval command = %q, want decoded Meta.Data", got)
	}
}

func TestEvalCommandPrefersEncryptedEvalDataParam(t *testing.T) {
	got, err := evalCommand(vmmSchema.Meta{Data: "bWV0YS1jb21tYW5k"}, map[string]string{
		"Data":      "",
		"Eval-Data": "encrypted-command",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "encrypted-command" {
		t.Fatalf("Eval command = %q, want encrypted Eval-Data param", got)
	}
}

func TestEvalCommandDecodesMetaDataFallback(t *testing.T) {
	got, err := evalCommand(vmmSchema.Meta{Data: "ZXhpdCAw"}, nil)
	if err != nil || got != "exit 0" {
		t.Fatalf("Eval fallback command = %q, err = %v", got, err)
	}
}

func TestEvalCommandRejectsInvalidMetaDataFallback(t *testing.T) {
	if _, err := evalCommand(vmmSchema.Meta{Data: "not base64!"}, nil); err == nil {
		t.Fatal("expected invalid Eval Meta.Data error")
	}
}

func TestRuntimeApplyRejectsNonOwner(t *testing.T) {
	starter := &fakeStarter{}
	rt := &Runtime{owner: "owner-1", starter: starter}
	if _, err := rt.Apply("attacker", vmmSchema.Meta{Action: startAgentAction}, nil); err == nil || !strings.Contains(err.Error(), "restricted to process owner") {
		t.Fatalf("expected owner restriction, got %v", err)
	}
	if starter.startCalls != 0 {
		t.Fatalf("starter called %d times for non-owner", starter.startCalls)
	}
}

func TestRuntimeCheckpointCanBeRestored(t *testing.T) {
	rt, err := NewWithParams("owner-1", nil)
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
	if _, err := NewRestored("owner-1", checkpoint, nil); err != nil {
		t.Fatalf("NewRestored failed: %v", err)
	}
}

func TestRuntimeRestoreRejectsUnknownFormat(t *testing.T) {
	if _, err := NewRestored("owner-1", `{"format":"unknown"}`, nil); err == nil || !strings.Contains(err.Error(), "unsupported hermes checkpoint format") {
		t.Fatalf("expected unsupported format error, got %v", err)
	}
}
