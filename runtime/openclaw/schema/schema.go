package schema

import "time"

type Endpoint struct {
	Method string
	Path   string
}

type Config struct {
	BaseURL         string
	Token           string
	Timeout         time.Duration
	ActionEndpoints map[string]Endpoint
}

type RuntimeState struct {
	SessionID string
}

type ToolInvokeRequest struct {
	Tool       string                 `json:"tool"`
	Action     string                 `json:"action,omitempty"`
	Args       map[string]interface{} `json:"args,omitempty"`
	SessionKey string                 `json:"sessionKey,omitempty"`
	DryRun     bool                   `json:"dryRun,omitempty"`
	// Arguments is kept for backward compatibility with older gateways expecting this key.
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type GatewayResponse struct {
	StatusCode int
	Status     string
	Data       string
	Body       string
	JSON       map[string]interface{}
}
