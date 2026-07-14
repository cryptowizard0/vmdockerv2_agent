package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cryptowizard0/vmdockerv2_agent/common"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
	goarSchema "github.com/permadao/goar/schema"
)

const (
	checkpointFormatV1  = "claudecode.runtime.v1"
	defaultTimeout      = 10 * time.Minute
	defaultClaudeBinary = "claude"
	maxApplyAttempts    = 2
)

var log = common.NewLog("claudecode")

type Config struct {
	Binary  string
	APIKey  string
	BaseURL string
	Model   string
	Flags   []string
	Cwd     string
	Timeout time.Duration
}

type Runtime struct {
	mu     sync.RWMutex
	config Config
	state  checkpointState
}

type checkpointState struct {
	Format    string `json:"format"`
	SessionID string `json:"sessionId,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`
	BaseURL   string `json:"baseURL,omitempty"`
}

type cliResult struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
}

func NewWithParams(spawnParams map[string]string) (*Runtime, error) {
	cfg, err := loadConfig(spawnParams)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		config: cfg,
		state: checkpointState{
			Format:  checkpointFormatV1,
			Cwd:     cfg.Cwd,
			Model:   cfg.Model,
			BaseURL: cfg.BaseURL,
		},
	}, nil
}

func NewRestored(state string, spawnParams map[string]string) (*Runtime, error) {
	cfg, err := loadConfig(spawnParams)
	if err != nil {
		return nil, err
	}

	rt := &Runtime{
		config: cfg,
		state: checkpointState{
			Format:  checkpointFormatV1,
			Cwd:     cfg.Cwd,
			Model:   cfg.Model,
			BaseURL: cfg.BaseURL,
		},
	}
	if err := rt.Restore(state); err != nil {
		return nil, err
	}
	return rt, nil
}

func loadConfig(spawnParams map[string]string) (Config, error) {
	binary := strings.TrimSpace(os.Getenv("CLAUDE_CODE_BIN"))
	if binary == "" {
		binary = defaultClaudeBinary
	}
	if _, err := exec.LookPath(binary); err != nil {
		return Config{}, fmt.Errorf("find claude binary %s failed: %w", binary, err)
	}

	cwd := strings.TrimSpace(os.Getenv("VMDOCKER_AGENT_WORKSPACE"))
	if cwd == "" {
		cwd = strings.TrimSpace(os.Getenv("VMDOCKER_RUNTIME_WORKSPACE"))
	}
	if cwd == "" {
		cwd = strings.TrimSpace(os.Getenv("OPENCLAW_AGENT_WORKSPACE"))
	}
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("resolve runtime workspace failed: %w", err)
		}
		cwd = wd
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return Config{}, fmt.Errorf("resolve runtime workspace %s failed: %w", cwd, err)
	}

	model := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL"))
	if model == "" {
		model = strings.TrimSpace(os.Getenv("CLAUDE_MODEL"))
	}
	if model == "" {
		model = extractModelName(spawnParams)
	}

	flags, err := parseFlags(os.Getenv("CLAUDE_CODE_FLAGS"))
	if err != nil {
		return Config{}, err
	}

	return Config{
		Binary:  binary,
		APIKey:  strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		BaseURL: strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")),
		Model:   model,
		Flags:   flags,
		Cwd:     cwd,
		Timeout: resolveTimeout(),
	}, nil
}

func resolveTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CLAUDE_CODE_TIMEOUT_MS"))
	if raw == "" {
		return defaultTimeout
	}
	timeoutMs, err := strconv.Atoi(raw)
	if err != nil || timeoutMs <= 0 {
		return defaultTimeout
	}
	return time.Duration(timeoutMs) * time.Millisecond
}

func parseFlags(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	args := make([]string, 0, 4)
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}

	for _, r := range raw {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if escaped {
		return nil, fmt.Errorf("parse CLAUDE_CODE_FLAGS failed: trailing escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("parse CLAUDE_CODE_FLAGS failed: unterminated quote")
	}
	flush()
	return args, nil
}

func extractModelName(params map[string]string) string {
	if params == nil {
		return ""
	}
	for _, key := range []string{"model", "Model", "modelName", "ModelName"} {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	return ""
}

func (r *Runtime) Apply(from string, meta vmmSchema.Meta, params map[string]string) (vmmSchema.Result, error) {
	if params == nil {
		params = map[string]string{}
	}

	prompt := extractCommand(meta, params)
	if prompt == "" {
		return vmmSchema.Result{}, fmt.Errorf("claude apply failed: command is empty")
	}

	cfg := r.snapshotConfig()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	sessionID := strings.TrimSpace(r.sessionID())
	var result cliResult
	var rawOutput string
	var err error
	for attempt := 1; attempt <= maxApplyAttempts; attempt++ {
		result, rawOutput, err = r.runCLI(ctx, cfg, prompt, sessionID)
		if err != nil {
			return vmmSchema.Result{}, err
		}
		if result.IsError {
			return vmmSchema.Result{}, fmt.Errorf("claude apply failed: %s", strings.TrimSpace(result.Result))
		}
		if strings.TrimSpace(result.Result) != "" {
			break
		}
		log.Warn("claude apply returned empty result", "attempt", attempt, "session_id", result.SessionID, "raw", rawOutput)
		if attempt == maxApplyAttempts {
			return vmmSchema.Result{}, fmt.Errorf("claude apply failed: successful response missing result text: %s", rawOutput)
		}
		if strings.TrimSpace(result.SessionID) != "" {
			sessionID = strings.TrimSpace(result.SessionID)
		}
	}

	r.updateState(result.SessionID, cfg)

	action := extractAction(meta, params)
	requestID := extractRequestID(meta, params)
	target := resolveTarget(from, meta, params)
	reply := strings.TrimSpace(result.Result)
	sessionID = r.sessionID()

	tags := []goarSchema.Tag{
		{Name: "Runtime", Value: "claude"},
		{Name: "SessionID", Value: sessionID},
		{Name: "Reference", Value: requestID},
		{Name: "Reply", Value: reply},
	}
	if action != "" {
		tags = append(tags, goarSchema.Tag{Name: "Action", Value: action})
	}
	for key, value := range params {
		if strings.HasPrefix(key, "X-") {
			tags = append(tags, goarSchema.Tag{Name: key, Value: value})
		}
	}

	outputPayload := interface{}(reply)
	if action == "Chat" {
		outputPayload = map[string]interface{}{
			"action": action,
			"reply":  reply,
		}
	}

	return vmmSchema.Result{
		Messages: []*vmmSchema.ResMessage{
			{
				Target: target,
				Data:   reply,
				Tags:   tags,
			},
		},
		Spawns:      []*vmmSchema.ResSpawn{},
		Assignments: nil,
		Output:      outputPayload,
		Data:        reply,
		Error:       nil,
	}, nil
}

func (r *Runtime) runCLI(ctx context.Context, cfg Config, prompt, sessionID string) (cliResult, string, error) {
	args := r.buildArgs(cfg.Model, prompt, sessionID)
	cmd := exec.CommandContext(ctx, cfg.Binary, args...)
	cmd.Dir = cfg.Cwd
	cmd.Env = r.buildEnv(cfg)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return cliResult{}, "", fmt.Errorf("claude apply failed: timeout after %s", cfg.Timeout)
	}
	if err != nil {
		errorOutput := strings.TrimSpace(stderr.String())
		if errorOutput == "" {
			errorOutput = strings.TrimSpace(stdout.String())
		}
		return cliResult{}, strings.TrimSpace(stdout.String()), fmt.Errorf("claude apply failed: %w: %s", err, errorOutput)
	}

	rawOutput := strings.TrimSpace(stdout.String())
	result, parseErr := parseCLIResult(stdout.Bytes())
	if parseErr != nil {
		return cliResult{}, rawOutput, fmt.Errorf("claude apply failed: %w", parseErr)
	}
	return result, rawOutput, nil
}

func (r *Runtime) buildArgs(model, prompt, sessionID string) []string {
	args := make([]string, 0, 10+len(r.config.Flags))
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	args = append(args, "-p", prompt, "--output-format", "json", "--dangerously-skip-permissions")
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	args = append(args, r.config.Flags...)
	return args
}

func (r *Runtime) buildEnv(cfg Config) []string {
	baseEnv := os.Environ()
	baseEnv = upsertEnv(baseEnv, "ANTHROPIC_API_KEY", cfg.APIKey)
	baseEnv = upsertEnv(baseEnv, "ANTHROPIC_BASE_URL", cfg.BaseURL)
	return baseEnv
}

func parseCLIResult(output []byte) (cliResult, error) {
	var result cliResult
	if err := json.Unmarshal(output, &result); err != nil {
		return cliResult{}, fmt.Errorf("decode claude json output failed: %w", err)
	}
	if strings.TrimSpace(result.Result) == "" && strings.TrimSpace(result.SessionID) == "" && !result.IsError {
		return cliResult{}, fmt.Errorf("claude json output missing result and session_id")
	}
	return result, nil
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	found := false
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			found = true
			if value == "" {
				continue
			}
			out = append(out, prefix+value)
			continue
		}
		out = append(out, item)
	}
	if !found && value != "" {
		out = append(out, prefix+value)
	}
	return out
}

func (r *Runtime) Checkpoint() (string, error) {
	r.mu.RLock()
	state := r.state
	r.mu.RUnlock()
	state.Format = checkpointFormatV1

	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal claude checkpoint failed: %w", err)
	}
	return string(payload), nil
}

func (r *Runtime) Restore(data string) error {
	if strings.TrimSpace(data) == "" {
		return fmt.Errorf("claude checkpoint state is empty")
	}

	var state checkpointState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return fmt.Errorf("decode claude checkpoint failed: %w", err)
	}
	if state.Format != "" && state.Format != checkpointFormatV1 {
		return fmt.Errorf("unsupported claude checkpoint format: %s", state.Format)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.state.Format = checkpointFormatV1
	r.state.SessionID = strings.TrimSpace(state.SessionID)
	if strings.TrimSpace(r.config.Cwd) != "" {
		r.state.Cwd = r.config.Cwd
	} else {
		r.state.Cwd = strings.TrimSpace(state.Cwd)
	}
	if strings.TrimSpace(r.config.Model) != "" {
		r.state.Model = r.config.Model
	} else {
		r.state.Model = strings.TrimSpace(state.Model)
	}
	if strings.TrimSpace(r.config.BaseURL) != "" {
		r.state.BaseURL = r.config.BaseURL
	} else {
		r.state.BaseURL = strings.TrimSpace(state.BaseURL)
	}
	return nil
}

func (r *Runtime) snapshotConfig() Config {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cfg := r.config
	if strings.TrimSpace(r.state.Cwd) != "" {
		cfg.Cwd = strings.TrimSpace(r.state.Cwd)
	}
	if strings.TrimSpace(r.state.Model) != "" {
		cfg.Model = strings.TrimSpace(r.state.Model)
	}
	if strings.TrimSpace(r.state.BaseURL) != "" {
		cfg.BaseURL = strings.TrimSpace(r.state.BaseURL)
	}
	return cfg
}

func (r *Runtime) updateState(sessionID string, cfg Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Format = checkpointFormatV1
	if strings.TrimSpace(sessionID) != "" {
		r.state.SessionID = strings.TrimSpace(sessionID)
	}
	r.state.Cwd = cfg.Cwd
	r.state.Model = cfg.Model
	r.state.BaseURL = cfg.BaseURL
}

func (r *Runtime) sessionID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return strings.TrimSpace(r.state.SessionID)
}

func resolveTarget(from string, meta vmmSchema.Meta, params map[string]string) string {
	target := strings.TrimSpace(from)
	if target == "" {
		target = strings.TrimSpace(params["From"])
	}
	if target == "" {
		target = strings.TrimSpace(meta.FromProcess)
	}
	return target
}

func extractAction(meta vmmSchema.Meta, params map[string]string) string {
	action := normalizeAction(meta.Action)
	if action != "" {
		return action
	}
	action = normalizeAction(params["action"])
	if action != "" {
		return action
	}
	action = normalizeAction(params["Action"])
	if action != "" {
		return action
	}
	return "Query"
}

func normalizeAction(action string) string {
	action = strings.TrimSpace(action)
	switch strings.ToLower(action) {
	case "chat":
		return "Chat"
	case "execute":
		return "Execute"
	case "query":
		return "Query"
	default:
		return action
	}
}

func extractRequestID(meta vmmSchema.Meta, params map[string]string) string {
	if ref := strings.TrimSpace(params["reference"]); ref != "" {
		return ref
	}
	if ref := strings.TrimSpace(params["Reference"]); ref != "" {
		return ref
	}
	if meta.Sequence > 0 {
		return strconv.FormatInt(meta.Sequence, 10)
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

func extractCommand(meta vmmSchema.Meta, params map[string]string) string {
	for _, key := range []string{"command", "Command", "prompt", "Prompt", "input", "Input", "data", "Data"} {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(meta.Data); value != "" {
		return value
	}
	return ""
}
