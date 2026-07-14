package telegramcustomer

import (
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
	customer "github.com/xingj404-lab/claude-gw/tg-customer"
)

const (
	checkpointFormatV1  = "telegramcustomer.runtime.v1"
	defaultTimeout      = 10 * time.Minute
	defaultClaudeBinary = "claude"
)

var log = common.NewLog("tgcustomer")
var RunByHymx = customer.RunByHymx

type Config struct {
	Binary   string
	APIKey   string
	BaseURL  string
	Model    string
	Flags    []string
	Cwd      string
	Timeout  time.Duration
	BotToken string
}

type checkpointState struct {
	Format    string `json:"format"`
	SessionID string `json:"sessionId,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`
	BaseURL   string `json:"baseURL,omitempty"`
}

type Runtime struct {
	mu     sync.RWMutex
	config Config
	state  checkpointState
}

func NewWithParams(spawnParams map[string]string) (*Runtime, error) {
	cfg, err := loadConfig(spawnParams)
	if err != nil {
		return nil, err
	}
	homeDir := strings.TrimSpace(os.Getenv("VMDOCKER_RUNTIME_HOME"))
	homeDir, _ = filepath.Abs(homeDir)

	if err = RunByHymx(cfg.Cwd, homeDir, cfg.BotToken); err != nil {
		return nil, err
	}
	return &Runtime{
		config: cfg,
		state: checkpointState{
			Format:    checkpointFormatV1,
			SessionID: newSessionID(),
			Cwd:       cfg.Cwd,
			Model:     cfg.Model,
			BaseURL:   cfg.BaseURL,
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
	homeDir := strings.TrimSpace(os.Getenv("VMDOCKER_RUNTIME_HOME"))
	homeDir, _ = filepath.Abs(homeDir)

	if err = RunByHymx(cfg.Cwd, homeDir, cfg.BotToken); err != nil {
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

	botToken := strings.TrimSpace(os.Getenv("BOT_TOKEN"))
	if botToken == "" {
		botToken = extractBotToken(spawnParams)
	}
	if botToken == "" {
		return Config{}, fmt.Errorf("BOT_TOKEN is required, set via BOT_TOKEN env or botToken tag")
	}

	return Config{
		Binary:   binary,
		APIKey:   strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		BaseURL:  strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")),
		Model:    model,
		Flags:    flags,
		Cwd:      cwd,
		Timeout:  resolveTimeout(),
		BotToken: botToken,
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

func extractBotToken(params map[string]string) string {
	if params == nil {
		return ""
	}
	for _, key := range []string{"botToken", "BotToken", "bot_token"} {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	return ""
}

func (r *Runtime) Apply(from string, meta vmmSchema.Meta, params map[string]string) (vmmSchema.Result, error) {
	return vmmSchema.Result{}, fmt.Errorf("telegramcustomer apply is not implemented")
}

func (r *Runtime) Checkpoint() (string, error) {
	r.mu.RLock()
	state := r.state
	r.mu.RUnlock()
	state.Format = checkpointFormatV1

	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal telegramcustomer checkpoint failed: %w", err)
	}
	return string(payload), nil
}

func (r *Runtime) Restore(data string) error {
	if strings.TrimSpace(data) == "" {
		return fmt.Errorf("telegramcustomer checkpoint state is empty")
	}

	var state checkpointState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return fmt.Errorf("decode telegramcustomer checkpoint failed: %w", err)
	}
	if state.Format != "" && state.Format != checkpointFormatV1 {
		return fmt.Errorf("unsupported telegramcustomer checkpoint format: %s", state.Format)
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

func newSessionID() string {
	return fmt.Sprintf("telegramcustomer-%d", time.Now().UnixNano())
}
