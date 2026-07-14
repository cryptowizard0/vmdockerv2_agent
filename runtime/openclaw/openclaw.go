package openclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cryptowizard0/vmdockerv2_agent/common"
	schema "github.com/cryptowizard0/vmdockerv2_agent/runtime/openclaw/schema"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
	goarSchema "github.com/permadao/goar/schema"
)

const (
	ActionPing              = "Ping"
	ActionQuery             = "Query"
	ActionExecute           = "Execute"
	ActionChat              = "Chat"
	ActionConfig            = "Config"
	ActionApprovePairing    = "ApprovePairing"
	ActionCreateSession     = "CreateSession"
	ActionCloseSession      = "CloseSession"
	ActionConfigureModel    = "ConfigureModel"
	ActionConfigureTelegram = "ConfigureTelegram"
)

var log = common.NewLog("openclaw")

const checkpointFormatV1 = "openclaw.runtime.v1"

type Openclaw struct {
	mu     sync.RWMutex
	client GatewayClient
	cfg    schema.Config
	state  schema.RuntimeState
}

func LoadConfigFromEnv() schema.Config {
	baseURL := getEnvOrDefault("OPENCLAW_GATEWAY_URL", "http://127.0.0.1:18789")
	timeoutMs, err := strconv.Atoi(getEnvOrDefault("OPENCLAW_TIMEOUT_MS", "30000"))
	if err != nil || timeoutMs <= 0 {
		timeoutMs = 30000
	}
	return schema.Config{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   os.Getenv("OPENCLAW_GATEWAY_TOKEN"),
		Timeout: time.Duration(timeoutMs) * time.Millisecond,
		ActionEndpoints: map[string]schema.Endpoint{
			ActionPing:              {Method: http.MethodGet, Path: getEnvOrDefault("OPENCLAW_ENDPOINT_PING", "/health")},
			ActionQuery:             {Method: http.MethodPost, Path: getEnvOrDefault("OPENCLAW_ENDPOINT_QUERY", "/tools/invoke")},
			ActionExecute:           {Method: http.MethodPost, Path: getEnvOrDefault("OPENCLAW_ENDPOINT_EXECUTE", "/tools/invoke")},
			ActionChat:              {Method: http.MethodPost, Path: getEnvOrDefault("OPENCLAW_ENDPOINT_CHAT", "/tools/invoke")},
			ActionConfig:            {Method: http.MethodPost, Path: getEnvOrDefault("OPENCLAW_ENDPOINT_CONFIG", "/tools/invoke")},
			ActionApprovePairing:    {Method: http.MethodPost, Path: getEnvOrDefault("OPENCLAW_ENDPOINT_APPROVE_PAIRING", "/tools/invoke")},
			ActionCreateSession:     {Method: http.MethodPost, Path: getEnvOrDefault("OPENCLAW_ENDPOINT_CREATE_SESSION", "/tools/invoke")},
			ActionCloseSession:      {Method: http.MethodPost, Path: getEnvOrDefault("OPENCLAW_ENDPOINT_CLOSE_SESSION", "/tools/invoke")},
			ActionConfigureModel:    {Method: http.MethodPost, Path: getEnvOrDefault("OPENCLAW_ENDPOINT_CONFIGURE_MODEL", "/tools/invoke")},
			ActionConfigureTelegram: {Method: http.MethodPost, Path: getEnvOrDefault("OPENCLAW_ENDPOINT_CONFIGURE_TELEGRAM", "/tools/invoke")},
		},
	}
}

func New() (*Openclaw, error) {
	return NewWithParams(nil)
}

func NewWithParams(spawnParams map[string]string) (*Openclaw, error) {
	rt, err := newRuntimeWithGateway()
	if err != nil {
		return nil, err
	}

	setupCtx, setupCancel := context.WithTimeout(context.Background(), rt.cfg.Timeout)
	defer setupCancel()
	if err := rt.SetupOnSpawn(setupCtx, spawnParams); err != nil {
		return nil, err
	}

	return rt, nil
}

func NewRestored(state string) (*Openclaw, error) {
	rt, err := newRuntimeWithGateway()
	if err != nil {
		return nil, err
	}
	if err := rt.Restore(state); err != nil {
		return nil, err
	}
	return rt, nil
}

func newRuntimeWithGateway() (*Openclaw, error) {
	cfg := LoadConfigFromEnv()
	client := NewHTTPGatewayClient(cfg)

	initCtx, initCancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer initCancel()
	if err := client.Init(initCtx); err != nil {
		return nil, err
	}

	rt := &Openclaw{
		client: client,
		cfg:    cfg,
		state:  schema.RuntimeState{},
	}

	return rt, nil
}

func (r *Openclaw) Apply(from string, meta vmmSchema.Meta, params map[string]string) (vmmSchema.Result, error) {
	if params == nil {
		params = map[string]string{}
	}

	action := extractAction(meta, params)
	requestID := extractRequestID(meta, params)
	target := from
	if target == "" {
		target = params["From"]
	}
	if target == "" {
		target = meta.FromProcess
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.Timeout)
	defer cancel()

	var (
		resp         *schema.GatewayResponse
		responseData string
		err          error
	)

	switch action {
	case ActionPing:
		resp, err = r.client.Call(ctx, ActionPing, nil)
		if err != nil {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: %w", err)
		}
	case ActionCreateSession:
		resp, err = r.CreateSession(ctx)
		if err != nil {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: %w", err)
		}
		responseData = r.sessionID()
	case ActionCloseSession:
		resp, responseData, err = r.closeSession(ctx)
		if err != nil {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: %w", err)
		}
	case ActionConfigureTelegram:
		resp, err = r.ConfigureTelegram(ctx, params)
		if err != nil {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: %w", err)
		}
	case ActionConfigureModel:
		model := extractModelName(meta, params)
		if model == "" {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: model is empty")
		}
		resp, err = r.ConfigureModel(ctx, model)
		if err != nil {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: %w", err)
		}
	case ActionConfig:
		resp, err = r.Configure(ctx, meta, params)
		if err != nil {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: %w", err)
		}
	case ActionApprovePairing:
		resp, err = r.ApprovePairing(ctx, meta, params)
		if err != nil {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: %w", err)
		}
	default:
		command := extractCommand(meta, params)
		if command == "" {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: command is empty")
		}

		sessionID := r.sessionID()
		if sessionID == "" {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: session is not initialized")
		}

		payload := schema.ToolInvokeRequest{
			Tool: resolveSendTool(action),
		}
		payloadArgs := map[string]interface{}{
			"sessionKey": sessionID,
			"message":    command,
		}
		if timeoutSeconds, ok := extractTimeoutSeconds(params); ok {
			payloadArgs["timeoutSeconds"] = timeoutSeconds
		}
		payload = newToolInvokeRequest(payload.Tool, payloadArgs, sessionID)

		resp, err = r.client.Call(ctx, action, payload)
		if err != nil {
			return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: %w", err)
		}
	}

	if resp == nil {
		return vmmSchema.Result{}, fmt.Errorf("openclaw apply failed: empty gateway response")
	}

	if responseData == "" {
		responseData = resp.Data
	}
	if responseData == "" {
		responseData = resp.Body
	}

	currentSessionID := r.sessionID()
	tags := []goarSchema.Tag{
		{Name: "Runtime", Value: "openclaw"},
		{Name: "GatewayStatus", Value: resp.Status},
		{Name: "StatusCode", Value: strconv.Itoa(resp.StatusCode)},
		{Name: "SessionID", Value: currentSessionID},
		{Name: "Reference", Value: requestID},
		{Name: "Reply", Value: responseData},
	}

	// Forward X- prefixed tags to both messages
	for key, value := range params {
		if strings.HasPrefix(key, "X-") {
			tags = append(tags, goarSchema.Tag{Name: key, Value: value})
		}
	}

	output := interface{}(responseData)
	if action == ActionChat {
		output = map[string]interface{}{
			"action": action,
			"reply":  responseData,
		}
	}

	result := vmmSchema.Result{
		Messages: []*vmmSchema.ResMessage{
			{
				Target: target,
				Data:   responseData,
				Tags:   tags,
			},
		},
		Spawns:      []*vmmSchema.ResSpawn{},
		Assignments: nil,
		Output:      output,
		Data:        responseData,
		Error:       nil,
	}

	log.Info("openclaw apply success", "action", action, "requestId", requestID, "statusCode", resp.StatusCode)
	return result, nil
}

// createSession moved to setup.go as CreateSession

func (r *Openclaw) closeSession(ctx context.Context) (*schema.GatewayResponse, string, error) {
	sessionID := r.sessionID()
	if sessionID == "" {
		return nil, "", fmt.Errorf("openclaw session is empty")
	}

	tool := getEnvOrDefault("OPENCLAW_TOOL_CLOSE_SESSION", DefaultToolCloseSession)
	resp, err := r.client.Call(ctx, ActionCloseSession, newToolInvokeRequest(tool, map[string]interface{}{
		"sessionKey": sessionID,
		"sessionId":  sessionID,
	}, sessionID))
	if err != nil {
		return nil, "", err
	}

	r.mu.Lock()
	r.state.SessionID = ""
	r.mu.Unlock()

	responseData := resp.Data
	if responseData == "" {
		responseData = "closed:" + sessionID
	}
	return resp, responseData, nil
}

type checkpointState struct {
	Format    string `json:"format"`
	SessionID string `json:"sessionId,omitempty"`
}

func (r *Openclaw) Checkpoint() (string, error) {
	sessionID := strings.TrimSpace(r.sessionID())
	if sessionID == "" {
		return "", fmt.Errorf("openclaw checkpoint sessionId is empty")
	}

	payload, err := json.Marshal(checkpointState{
		Format:    checkpointFormatV1,
		SessionID: sessionID,
	})
	if err != nil {
		return "", fmt.Errorf("marshal openclaw checkpoint failed: %w", err)
	}
	return string(payload), nil
}

func (r *Openclaw) Restore(data string) error {
	if strings.TrimSpace(data) == "" {
		return fmt.Errorf("openclaw checkpoint state is empty")
	}

	var state checkpointState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return fmt.Errorf("decode openclaw checkpoint failed: %w", err)
	}
	if state.Format != "" && state.Format != checkpointFormatV1 {
		return fmt.Errorf("unsupported openclaw checkpoint format: %s", state.Format)
	}
	if strings.TrimSpace(state.SessionID) == "" {
		return fmt.Errorf("openclaw checkpoint sessionId is empty")
	}

	r.mu.Lock()
	r.state.SessionID = strings.TrimSpace(state.SessionID)
	r.mu.Unlock()
	return nil
}

func (r *Openclaw) sessionID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state.SessionID
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
	return ActionQuery
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

func normalizeAction(action string) string {
	action = strings.TrimSpace(action)
	switch strings.ToLower(action) {
	case "ping":
		return ActionPing
	case "query":
		return ActionQuery
	case "execute":
		return ActionExecute
	case "chat":
		return ActionChat
	case "config", "configure", "configureconfig", "setconfig":
		return ActionConfig
	case "approvepairing", "approvepair", "pairingapprove", "approvetelegrampairing":
		return ActionApprovePairing
	case "createsession":
		return ActionCreateSession
	case "closesession":
		return ActionCloseSession
	case "configuremodel", "setmodel":
		return ActionConfigureModel
	case "configuretelegram", "telegramconfig", "settelegram":
		return ActionConfigureTelegram
	default:
		return action
	}
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

func extractTimeoutSeconds(params map[string]string) (int, bool) {
	for _, key := range []string{"timeoutSeconds", "TimeoutSeconds"} {
		if value := strings.TrimSpace(params[key]); value != "" {
			seconds, err := strconv.Atoi(value)
			if err == nil && seconds >= 0 {
				return seconds, true
			}
		}
	}
	return 0, false
}

func loadSessionMetadataFromEnv() (map[string]interface{}, error) {
	raw := strings.TrimSpace(os.Getenv("OPENCLAW_SESSION_METADATA_JSON"))
	if raw == "" {
		return nil, nil
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil, fmt.Errorf("invalid OPENCLAW_SESSION_METADATA_JSON: %w", err)
	}
	return metadata, nil
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func toJSONString(v interface{}) string {
	buf, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(buf)
}

func getEnvOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
