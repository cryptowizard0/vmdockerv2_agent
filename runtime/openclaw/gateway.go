package openclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	schema "github.com/cryptowizard0/vmdockerv2_agent/runtime/openclaw/schema"
)

type GatewayClient interface {
	Init(ctx context.Context) error
	Call(ctx context.Context, action string, payload interface{}) (*schema.GatewayResponse, error)
	Close(ctx context.Context) error
}

type HTTPGatewayClient struct {
	cfg    schema.Config
	client *http.Client
}

func NewHTTPGatewayClient(cfg schema.Config) *HTTPGatewayClient {
	return &HTTPGatewayClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (c *HTTPGatewayClient) Init(ctx context.Context) error {
	resp, err := c.Call(ctx, ActionPing, nil)
	if err != nil {
		return fmt.Errorf("openclaw gateway init failed: %w", err)
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("openclaw gateway unhealthy status: %d", resp.StatusCode)
	}
	return nil
}

func (c *HTTPGatewayClient) Call(ctx context.Context, action string, payload interface{}) (*schema.GatewayResponse, error) {
	action = normalizeAction(action)
	ep, ok := c.cfg.ActionEndpoints[action]
	if !ok {
		return nil, fmt.Errorf("openclaw action not supported: %s", action)
	}

	url := c.cfg.BaseURL + normalizePath(ep.Path)
	var bodyReader *bytes.Reader
	if ep.Method == http.MethodPost {
		if payload == nil {
			payload = map[string]interface{}{}
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal openclaw payload failed: %w", err)
		}
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, ep.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create openclaw request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.Token != "" {
		req.Header.Set("x-gateway-token", c.cfg.Token)
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call openclaw gateway failed: %w", err)
	}
	defer res.Body.Close()

	var response schema.GatewayResponse
	response.StatusCode = res.StatusCode
	response.Status = res.Status

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("read openclaw gateway response failed: %w", err)
	}
	response.Body = strings.TrimSpace(string(bodyBytes))

	if len(bodyBytes) > 0 {
		var bodyMap map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &bodyMap); err == nil {
			response.JSON = bodyMap
			response.Body = toJSONString(bodyMap)
			response.Data = extractData(bodyMap)
		} else {
			response.Data = response.Body
		}
	}

	if res.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("openclaw gateway error status=%d body=%s", res.StatusCode, response.Body)
	}

	if response.Data == "" {
		response.Data = response.Body
	}

	return &response, nil
}

func (c *HTTPGatewayClient) Close(_ context.Context) error {
	return nil
}
