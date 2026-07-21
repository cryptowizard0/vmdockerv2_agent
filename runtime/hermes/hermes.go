package hermes

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	vmmSchema "github.com/hymatrix/hymx/vmm/schema"
)

const (
	checkpointFormatV2 = "hermes.gateway.runtime.v2"
	defaultGatewayURL  = "http://127.0.0.1:8642"
	defaultTimeout     = 10 * time.Minute
)

type Runtime struct{}

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

func NewWithParams(_ map[string]string) (*Runtime, error) {
	return &Runtime{}, nil
}

func NewRestored(state string, _ map[string]string) (*Runtime, error) {
	rt := &Runtime{}
	if err := rt.Restore(state); err != nil {
		return nil, err
	}
	return rt, nil
}

func (*Runtime) Apply(string, vmmSchema.Meta, map[string]string) (vmmSchema.Result, error) {
	return vmmSchema.Result{}, fmt.Errorf("hermes gateway runtime does not support Apply; use the Hermes service directly")
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
