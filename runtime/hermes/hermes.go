package hermes

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

const (
	checkpointFormatV2  = "hermes.gateway.runtime.v2"
	defaultGatewayURL   = "http://127.0.0.1:8642"
	defaultTimeout      = 10 * time.Minute
	userProgramDir      = "/usr/local/bin"
	defaultStartProgram = "start-hermes"
	startAgentAction    = "Start-Agent"
	evalAction          = "Eval"
	agentProgramParam   = "Agent-Program"
	containerEnvPrefix  = "Container-Env-"
)

type agentStarter interface {
	Start(string, map[string]string) error
}

type Runtime struct {
	mu      sync.Mutex
	owner   string
	started bool
	starter agentStarter
}

type commandStarter struct {
	logPath string
}

func (s commandStarter) Start(program string, overrides map[string]string) error {
	logFile, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open start-Hermes log: %w", err)
	}
	cmd := exec.Command(filepath.Join(userProgramDir, program))
	cmd.Env = mergeEnv(os.Environ(), overrides)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start user command: %w", err)
	}
	_ = logFile.Close()
	return nil
}

type checkpointState struct {
	Format string `json:"format"`
}

func LoadConfig(_ map[string]string) Config {
	return Config{
		URL:     firstNonEmpty(os.Getenv("HERMES_GATEWAY_URL"), defaultGatewayURL),
		Token:   firstNonEmpty(os.Getenv("HERMES_GATEWAY_TOKEN"), os.Getenv("API_SERVER_KEY")),
		Timeout: resolveTimeout(),
	}
}

func NewWithParams(owner string, _ map[string]string) (*Runtime, error) {
	return newRuntime(owner), nil
}

func NewRestored(owner, state string, _ map[string]string) (*Runtime, error) {
	rt := newRuntime(owner)
	if err := rt.Restore(state); err != nil {
		return nil, err
	}
	return rt, nil
}

func newRuntime(owner string) *Runtime {
	logPath := firstNonEmpty(os.Getenv("HERMES_START_AGENT_LOG"), "/tmp/start-hermes.log")
	return &Runtime{
		owner:   strings.TrimSpace(owner),
		starter: commandStarter{logPath: logPath},
	}
}

func (r *Runtime) Apply(from string, meta vmmSchema.Meta, params map[string]string) (vmmSchema.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owner == "" || from != r.owner {
		return vmmSchema.Result{}, fmt.Errorf("Hermes action %q is restricted to process owner", meta.Action)
	}

	switch meta.Action {
	case startAgentAction:
		if r.started {
			return vmmSchema.Result{Data: `{"status":"already_started"}`}, nil
		}
		program, err := startAgentProgram(params)
		if err != nil {
			return vmmSchema.Result{}, err
		}
		env := startAgentEnv(params)
		if err := r.starter.Start(program, env); err != nil {
			return vmmSchema.Result{}, fmt.Errorf("start Hermes agent: %w", err)
		}
		r.started = true
		return vmmSchema.Result{Data: `{"status":"started"}`}, nil
	case evalAction:
		command, err := evalCommand(meta, params)
		if err != nil {
			return vmmSchema.Result{}, err
		}
		if err := eval(command); err != nil {
			return vmmSchema.Result{}, err
		}
		return vmmSchema.Result{Data: `{"status":"started"}`}, nil
	}

	return vmmSchema.Result{}, nil
}

func evalCommand(meta vmmSchema.Meta, params map[string]string) (string, error) {
	if command, ok := params["Eval-Data"]; ok {
		return command, nil
	}

	command, err := base64.RawURLEncoding.DecodeString(meta.Data)
	if err != nil {
		return "", fmt.Errorf("decode Eval command: %w", err)
	}
	return string(command), nil
}

func startAgentProgram(params map[string]string) (string, error) {
	program := strings.TrimSpace(params[agentProgramParam])
	if program == "" {
		return defaultStartProgram, nil
	}
	if filepath.Base(program) != program || program == "." || program == ".." {
		return "", fmt.Errorf("%s must be a program name, got %q", agentProgramParam, program)
	}
	return program, nil
}

func eval(command string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("Eval command is empty")
	}
	cmd := exec.Command("sh", "-lc", command)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Eval command: %w", err)
	}
	return nil
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	env := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		key := strings.SplitN(item, "=", 2)[0]
		if _, replaced := overrides[key]; !replaced {
			env = append(env, item)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func startAgentEnv(params map[string]string) map[string]string {
	env := make(map[string]string, len(params))
	for key, value := range params {
		if !strings.HasPrefix(key, containerEnvPrefix) {
			continue
		}
		envKey := strings.TrimSpace(strings.TrimPrefix(key, containerEnvPrefix))
		if envKey != "" {
			env[envKey] = value
		}
	}
	return env
}

func (*Runtime) Checkpoint() (string, error) {
	payload, err := json.Marshal(checkpointState{Format: checkpointFormatV2})
	if err != nil {
		return "", fmt.Errorf("marshal hermes checkpoint failed: %w", err)
	}
	return string(payload), nil
}

func (*Runtime) Restore(data string) error {
	if strings.TrimSpace(data) == "" {
		return fmt.Errorf("hermes checkpoint state is empty")
	}
	var state checkpointState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return fmt.Errorf("decode hermes checkpoint failed: %w", err)
	}
	if state.Format != checkpointFormatV2 {
		return fmt.Errorf("unsupported hermes checkpoint format: %s", state.Format)
	}
	return nil
}

func resolveTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("HERMES_GATEWAY_TIMEOUT_MS"))
	if raw == "" {
		return defaultTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return defaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
