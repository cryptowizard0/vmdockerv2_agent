package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

type Config struct {
	URL        string
	Token      string
	Model      string
	SessionKey string
	Timeout    time.Duration
}

type GatewayClient interface {
	Ready(ctx context.Context) error
	Chat(ctx context.Context, sessionID, prompt string) (reply string, effectiveSessionID string, err error)
}

type HTTPGatewayClient struct {
	config Config
	client *http.Client
}

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func NewGatewayClient(config Config) *HTTPGatewayClient {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &HTTPGatewayClient{
		config: config,
		client: &http.Client{Timeout: timeout},
	}
}

func (c *HTTPGatewayClient) Ready(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("hermes gateway health request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError("hermes gateway health request", resp)
	}
	return nil
}

func (c *HTTPGatewayClient) Chat(ctx context.Context, sessionID, prompt string) (string, string, error) {
	body, err := json.Marshal(chatCompletionRequest{
		Model:    c.config.Model,
		Messages: []chatMessage{{Role: "user", Content: prompt}},
		Stream:   false,
	})
	if err != nil {
		return "", "", fmt.Errorf("encode hermes gateway request: %w", err)
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hermes-Session-Id", sessionID)
	if c.config.SessionKey != "" {
		req.Header.Set("X-Hermes-Session-Key", c.config.SessionKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("hermes gateway chat request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", responseError("hermes gateway chat request", resp)
	}
	var result chatCompletionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decode hermes gateway response: %w", err)
	}
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", "", fmt.Errorf("hermes gateway response missing assistant content")
	}
	effectiveSessionID := strings.TrimSpace(resp.Header.Get("X-Hermes-Session-Id"))
	if effectiveSessionID == "" {
		effectiveSessionID = sessionID
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), effectiveSessionID, nil
}

func (c *HTTPGatewayClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.config.URL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("hermes gateway URL is empty")
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("create hermes gateway request: %w", err)
	}
	if c.config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.Token)
	}
	return req, nil
}

func responseError(operation string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("%s failed with status %d: %s", operation, resp.StatusCode, detail)
}
