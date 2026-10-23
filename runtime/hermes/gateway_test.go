package hermes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientReadyChecksGatewayHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewGatewayClient(Config{URL: server.URL, Token: "secret"})
	if err := client.Ready(context.Background()); err != nil {
		t.Fatalf("Ready failed: %v", err)
	}
}

func TestClientReadyReportsGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gateway starting", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewGatewayClient(Config{URL: server.URL})
	err := client.Ready(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status 503: gateway starting") {
		t.Fatalf("expected gateway status error, got %v", err)
	}
}

func TestClientReadyRejectsEmptyURL(t *testing.T) {
	client := NewGatewayClient(Config{})
	if err := client.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "URL is empty") {
		t.Fatalf("expected empty URL error, got %v", err)
	}
}
