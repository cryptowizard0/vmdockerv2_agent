package hermes

import (
	"context"
	"net/http"
	"net/http/httptest"
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
