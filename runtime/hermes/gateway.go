package hermes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

type Config struct {
	URL     string
	Token   string
	Timeout time.Duration
}

type GatewayClient interface {
	Ready(ctx context.Context) error
}

type HTTPGatewayClient struct {
	config Config
	client *http.Client
}

func NewGatewayClient(config Config) *HTTPGatewayClient {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &HTTPGatewayClient{
		config: config,
		client: &http.Client{Timeout: timeout},
	}
}

func (c *HTTPGatewayClient) Ready(ctx context.Context) error {
	baseURL := strings.TrimRight(strings.TrimSpace(c.config.URL), "/")
	if baseURL == "" {
		return fmt.Errorf("hermes gateway URL is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("create hermes gateway health request: %w", err)
	}
	if c.config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.Token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("hermes gateway health request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			detail = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("hermes gateway health request failed with status %d: %s", resp.StatusCode, detail)
	}
	return nil
}
