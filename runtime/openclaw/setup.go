package openclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	schema "github.com/cryptowizard0/vmdockerv2_agent/runtime/openclaw/schema"
	"github.com/cryptowizard0/vmdockerv2_agent/utils"
	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

func (r *Openclaw) SetupOnSpawn(ctx context.Context, params map[string]string) error {
	if _, err := r.CreateSession(ctx); err != nil {
		return err
	}

	if apiKey := extractModelAPIKey(params); apiKey != "" {
		provider := extractProviderName(vmmSchema.Meta{}, params)
		if provider == "" {
			return fmt.Errorf("provider is required when model apiKey is provided")
		}
		profileID := extractAuthProfileID(provider, params)
		if profileID == "" {
			return fmt.Errorf("auth profile id is empty")
		}
		if err := upsertAuthProfileAPIKey(provider, profileID, apiKey); err != nil {
			return err
		}
		_ = runSecretsReload(ctx)
	}

	if model := extractModelName(vmmSchema.Meta{}, params); model != "" {
		sessionID := r.sessionID()
		fallbackSessionID := getEnvOrDefault("OPENCLAW_SESSION_KEY", "main")
		if strings.TrimSpace(sessionID) == strings.TrimSpace(fallbackSessionID) {
			log.Warn("skip configure model on fallback session", "sessionId", sessionID, "model", model)
		} else {
			if _, err := r.ConfigureModel(ctx, model); err != nil {
				return err
			}
		}
	}

	if len(buildTelegramConfigPatch(params)) > 0 {
		if _, err := r.ConfigureTelegram(ctx, params); err != nil {
			return err
		}
	}

	return nil
}

func (r *Openclaw) ConfigureModel(ctx context.Context, model string) (*schema.GatewayResponse, error) {
	if model == "" {
		return nil, fmt.Errorf("model is empty")
	}
	sessionID := r.sessionID()
	if sessionID == "" {
		return nil, fmt.Errorf("session is not initialized")
	}
	tool := resolveSetModelTool()
	args := map[string]interface{}{
		"sessionKey": sessionID,
		"model":      model,
	}
	payload := newToolInvokeRequest(tool, args, sessionID)
	resp, err := r.client.Call(ctx, ActionConfigureModel, payload)
	if err != nil {
		return nil, fmt.Errorf("configure model failed: %w", err)
	}
	return resp, nil
}

func resolveAuthStorePath() string {
	if p := strings.TrimSpace(os.Getenv("OPENCLAW_AUTH_STORE_PATH")); p != "" {
		return p
	}
	stateDir := utils.ResolveOpenclawStateDir(os.Getenv, os.UserHomeDir)
	return filepath.Join(stateDir, "agents", "main", "agent", "auth-profiles.json")
}

func upsertAuthProfileAPIKey(provider, profileID, apiKey string) error {
	provider = strings.TrimSpace(strings.ToLower(provider))
	profileID = strings.TrimSpace(profileID)
	apiKey = strings.TrimSpace(apiKey)
	if provider == "" {
		return fmt.Errorf("provider is empty")
	}
	if profileID == "" {
		return fmt.Errorf("profile id is empty")
	}
	if apiKey == "" {
		return fmt.Errorf("apiKey is empty")
	}

	authPath := resolveAuthStorePath()
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		fallbackPath := filepath.Join("/tmp/.openclaw", "agents", "main", "agent", "auth-profiles.json")
		if authPath == fallbackPath {
			return fmt.Errorf("create auth store dir failed: %w", err)
		}
		log.Warn("auth store dir create failed, fallback to /tmp", "path", authPath, "err", err)
		authPath = fallbackPath
		if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
			return fmt.Errorf("create fallback auth store dir failed: %w", err)
		}
	}

	store := map[string]interface{}{
		"version":  1,
		"profiles": map[string]interface{}{},
	}
	if raw, err := os.ReadFile(authPath); err == nil && len(raw) > 0 {
		var loaded map[string]interface{}
		if err := json.Unmarshal(raw, &loaded); err == nil {
			store = loaded
		}
	}
	if _, ok := store["version"]; !ok {
		store["version"] = 1
	}
	profiles, ok := store["profiles"].(map[string]interface{})
	if !ok || profiles == nil {
		profiles = map[string]interface{}{}
		store["profiles"] = profiles
	}
	profiles[profileID] = map[string]interface{}{
		"type":     "api_key",
		"provider": provider,
		"key":      apiKey,
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal auth store failed: %w", err)
	}
	tmp := authPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write auth store tmp failed: %w", err)
	}
	if err := os.Rename(tmp, authPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("move auth store tmp failed: %w", err)
	}
	return nil
}

func runSecretsReload(ctx context.Context) error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OPENCLAW_SECRETS_RELOAD")), "false") {
		return nil
	}
	cmd := exec.CommandContext(ctx, "openclaw", "secrets", "reload", "--json")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		log.Warn("openclaw secrets reload start failed", "err", err)
		return err
	}
	_, _ = io.ReadAll(stdout)
	_, _ = io.ReadAll(stderr)
	if err := cmd.Wait(); err != nil {
		log.Warn("openclaw secrets reload failed", "err", err)
		return err
	}
	return nil
}

func (r *Openclaw) ConfigureTelegram(ctx context.Context, params map[string]string) (*schema.GatewayResponse, error) {
	patch := buildTelegramConfigPatch(params)
	if len(patch) == 0 {
		return nil, fmt.Errorf("empty telegram patch")
	}
	rawPatch, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("marshal telegram patch failed: %w", err)
	}
	args := map[string]interface{}{
		"action": "config.patch",
		"raw":    string(rawPatch),
	}
	payload := newToolInvokeRequest(resolveGatewayTool(), args, "")
	resp, err := r.client.Call(ctx, ActionConfigureTelegram, payload)
	if err != nil {
		return nil, fmt.Errorf("configure telegram failed: %w", err)
	}
	return resp, nil
}

func (r *Openclaw) Configure(ctx context.Context, meta vmmSchema.Meta, params map[string]string) (*schema.GatewayResponse, error) {
	raw := extractConfigRaw(meta, params)
	if raw == "" {
		return nil, fmt.Errorf("config raw is empty")
	}

	configAction := extractConfigAction(params)
	args := map[string]interface{}{
		"action": configAction,
		"raw":    raw,
	}

	baseHash := extractConfigBaseHash(params)
	if baseHash == "" && configAction == "config.patch" {
		hash, err := r.fetchCurrentConfigHash(ctx)
		if err != nil {
			return nil, fmt.Errorf("read current config hash failed: %w", err)
		}
		if hash != "" {
			baseHash = hash
		}
	}
	if baseHash != "" {
		args["baseHash"] = baseHash
	}

	payload := newToolInvokeRequest(resolveGatewayTool(), args, "")
	resp, err := r.client.Call(ctx, ActionConfig, payload)
	if err != nil {
		return nil, fmt.Errorf("configure failed: %w", err)
	}
	return resp, nil
}

func (r *Openclaw) ApprovePairing(ctx context.Context, meta vmmSchema.Meta, params map[string]string) (*schema.GatewayResponse, error) {
	channel := extractPairingChannel(params)
	code := extractPairingCode(meta, params)
	if code == "" {
		return nil, fmt.Errorf("pairing code is empty")
	}

	// Prefer CLI command because some OpenClaw versions do not expose pairing.approve via gateway action.
	if out, err := runPairingApprove(ctx, channel, code); err == nil {
		output := strings.TrimSpace(out)
		return &schema.GatewayResponse{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Data:       output,
			Body:       output,
			JSON: map[string]interface{}{
				"ok":     true,
				"result": output,
			},
		}, nil
	}

	args := map[string]interface{}{
		"action":  "pairing.approve",
		"channel": channel,
		"code":    code,
	}
	payload := newToolInvokeRequest(resolveGatewayTool(), args, "")
	resp, err := r.client.Call(ctx, ActionApprovePairing, payload)
	if err != nil {
		return nil, fmt.Errorf("approve pairing failed: %w", err)
	}
	return resp, nil
}

func runPairingApprove(ctx context.Context, channel, code string) (string, error) {
	// Use a shell entrypoint to match environments where openclaw is resolved via shell init/profile.
	cmd := exec.CommandContext(ctx, "bash", "-lc", "openclaw pairing approve \"$1\" \"$2\"", "_", channel, code)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("openclaw pairing approve failed: %s", text)
	}
	return text, nil
}

func (r *Openclaw) fetchCurrentConfigHash(ctx context.Context) (string, error) {
	payload := newToolInvokeRequest(resolveGatewayTool(), map[string]interface{}{
		"action": "config.get",
	}, "")
	resp, err := r.client.Call(ctx, ActionConfig, payload)
	if err != nil {
		return "", err
	}
	return extractConfigHash(resp.JSON), nil
}

func (r *Openclaw) CreateSession(ctx context.Context) (*schema.GatewayResponse, error) {
	tool := getEnvOrDefault("OPENCLAW_TOOL_CREATE_SESSION", DefaultToolCreateSession)
	title := strings.TrimSpace(os.Getenv("OPENCLAW_SESSION_TITLE"))
	metadata, err := loadSessionMetadataFromEnv()
	if err != nil {
		return nil, err
	}

	args := map[string]interface{}{}
	if title != "" {
		args["title"] = title
	}
	if len(metadata) > 0 {
		args["metadata"] = metadata
	}

	resp, err := r.client.Call(ctx, ActionCreateSession, newToolInvokeRequest(tool, args, ""))
	if err != nil {
		if strings.Contains(err.Error(), "Tool not available") {
			fallbackSessionKey := getEnvOrDefault("OPENCLAW_SESSION_KEY", "main")
			r.mu.Lock()
			r.state.SessionID = fallbackSessionKey
			r.mu.Unlock()
			log.Warn("sessions_create not available, fallback to configured session key", "sessionKey", fallbackSessionKey)
			return &schema.GatewayResponse{
				StatusCode: http.StatusOK,
				Status:     "200 session created (fallback)",
				Data:       fallbackSessionKey,
				Body:       fallbackSessionKey,
				JSON: map[string]interface{}{
					"sessionId": fallbackSessionKey,
				},
			}, nil
		}
		return nil, fmt.Errorf("create openclaw session failed: %w", err)
	}

	sessionID := extractSessionID(resp.JSON)
	if sessionID == "" {
		sessionID = strings.TrimSpace(resp.Data)
	}
	if sessionID == "" {
		return nil, fmt.Errorf("create openclaw session failed: empty session id in response")
	}

	r.mu.Lock()
	r.state.SessionID = sessionID
	r.mu.Unlock()

	log.Info("openclaw session created", "sessionId", sessionID)
	return resp, nil
}
