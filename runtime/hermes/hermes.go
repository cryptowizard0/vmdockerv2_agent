package hermes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
	goarSchema "github.com/permadao/goar/schema"
)

const (
	checkpointFormatV1 = "hermes.gateway.runtime.v1"
	defaultGatewayURL  = "http://127.0.0.1:8642"
	defaultModel       = "hermes-agent"
	defaultTimeout     = 10 * time.Minute
)

type Runtime struct {
	mu        sync.RWMutex
	client    GatewayClient
	sessionID string
}

type checkpointState struct {
	Format    string `json:"format"`
	SessionID string `json:"sessionId"`
}

func LoadConfig(spawnParams map[string]string) Config {
	return Config{
		URL:        firstNonEmpty(os.Getenv("HERMES_GATEWAY_URL"), defaultGatewayURL),
		Token:      firstNonEmpty(os.Getenv("HERMES_GATEWAY_TOKEN"), os.Getenv("API_SERVER_KEY")),
		Model:      firstNonEmpty(os.Getenv("HERMES_GATEWAY_MODEL"), lookupParam(spawnParams, "model", "Model", "modelName", "ModelName"), defaultModel),
		SessionKey: firstNonEmpty(os.Getenv("HERMES_SESSION_KEY"), lookupParam(spawnParams, "sessionKey", "SessionKey")),
		Timeout:    resolveTimeout(),
	}
}

func NewWithParams(spawnParams map[string]string) (*Runtime, error) {
	sessionID, err := newSessionID()
	if err != nil {
		return nil, fmt.Errorf("create hermes gateway session ID: %w", err)
	}
	return &Runtime{client: NewGatewayClient(LoadConfig(spawnParams)), sessionID: sessionID}, nil
}

func NewRestored(state string, spawnParams map[string]string) (*Runtime, error) {
	rt := &Runtime{client: NewGatewayClient(LoadConfig(spawnParams))}
	if err := rt.Restore(state); err != nil {
		return nil, err
	}
	return rt, nil
}

func (r *Runtime) Apply(from string, meta vmmSchema.Meta, params map[string]string) (vmmSchema.Result, error) {
	if params == nil {
		params = map[string]string{}
	}
	prompt := extractCommand(meta, params)
	if prompt == "" {
		return vmmSchema.Result{}, fmt.Errorf("hermes apply failed: command is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout())
	defer cancel()
	reply, effectiveSessionID, err := r.client.Chat(ctx, r.currentSessionID(), prompt)
	if err != nil {
		return vmmSchema.Result{}, fmt.Errorf("hermes apply failed: %w", err)
	}
	r.setSessionID(effectiveSessionID)
	return buildResult(from, meta, params, reply, r.currentSessionID()), nil
}

func (r *Runtime) Checkpoint() (string, error) {
	state := checkpointState{Format: checkpointFormatV1, SessionID: r.currentSessionID()}
	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal hermes checkpoint failed: %w", err)
	}
	return string(payload), nil
}

func (r *Runtime) Restore(data string) error {
	if strings.TrimSpace(data) == "" {
		return fmt.Errorf("hermes checkpoint state is empty")
	}
	var state checkpointState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return fmt.Errorf("decode hermes checkpoint failed: %w", err)
	}
	if state.Format != "" && state.Format != checkpointFormatV1 {
		return fmt.Errorf("unsupported hermes checkpoint format: %s", state.Format)
	}
	if strings.TrimSpace(state.SessionID) == "" {
		return fmt.Errorf("hermes checkpoint session ID is empty")
	}
	r.mu.Lock()
	r.sessionID = strings.TrimSpace(state.SessionID)
	r.mu.Unlock()
	return nil
}

func (r *Runtime) currentSessionID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessionID
}

func (r *Runtime) setSessionID(sessionID string) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return
	}
	r.mu.Lock()
	r.sessionID = sessionID
	r.mu.Unlock()
}

func newSessionID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "vmdocker-" + hex.EncodeToString(raw), nil
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

func buildResult(from string, meta vmmSchema.Meta, params map[string]string, reply, sessionID string) vmmSchema.Result {
	action := extractAction(meta, params)
	tags := []goarSchema.Tag{
		{Name: "Runtime", Value: "hermes"},
		{Name: "SessionID", Value: sessionID},
		{Name: "Reference", Value: extractRequestID(meta, params)},
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
	output := any(reply)
	if action == "Chat" {
		output = map[string]any{"action": action, "reply": reply}
	}
	return vmmSchema.Result{
		Messages: []*vmmSchema.ResMessage{{Target: resolveTarget(from, meta, params), Data: reply, Tags: tags}},
		Spawns:   []*vmmSchema.ResSpawn{},
		Output:   output,
		Data:     reply,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func lookupParam(params map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(params[key]); value != "" {
			return value
		}
	}
	return ""
}

func resolveTarget(from string, meta vmmSchema.Meta, params map[string]string) string {
	return firstNonEmpty(from, params["From"], meta.FromProcess)
}

func extractAction(meta vmmSchema.Meta, params map[string]string) string {
	action := firstNonEmpty(meta.Action, params["action"], params["Action"])
	switch strings.ToLower(action) {
	case "chat":
		return "Chat"
	case "execute":
		return "Execute"
	case "query", "":
		return "Query"
	default:
		return action
	}
}

func extractRequestID(meta vmmSchema.Meta, params map[string]string) string {
	if ref := firstNonEmpty(params["reference"], params["Reference"]); ref != "" {
		return ref
	}
	if meta.Sequence > 0 {
		return strconv.FormatInt(meta.Sequence, 10)
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

func extractCommand(meta vmmSchema.Meta, params map[string]string) string {
	return firstNonEmpty(lookupParam(params, "command", "Command", "prompt", "Prompt", "input", "Input", "data", "Data"), meta.Data)
}
