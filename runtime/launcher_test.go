// runtime/launcher_test.go
package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCurrentRuntimeTypeDefaultsToTest(t *testing.T) {
	t.Setenv("RUNTIME_TYPE", "")
	if got := CurrentRuntimeType(); got != RuntimeTypeTest {
		t.Fatalf("want %q, got %q", RuntimeTypeTest, got)
	}
}

func TestLauncherForClaudeReadyRequiresCLI(t *testing.T) {
	l := LauncherFor(RuntimeTypeClaude)

	// Empty PATH -> claude not found -> not ready.
	t.Setenv("PATH", "")
	if err := l.Ready(context.Background()); err == nil {
		t.Fatal("want not-ready when claude is absent, got nil")
	}

	// A dir containing an executable named "claude" -> ready.
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/claude", []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := l.Ready(context.Background()); err != nil {
		t.Fatalf("want ready when claude present, got %v", err)
	}
}

func TestLauncherForHermesChecksGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	t.Setenv("HERMES_GATEWAY_URL", server.URL)

	if err := LauncherFor(RuntimeTypeHermes).Ready(context.Background()); err != nil {
		t.Fatalf("Hermes gateway should be ready: %v", err)
	}
}

func TestLauncherForTestAlwaysReady(t *testing.T) {
	l := LauncherFor(RuntimeTypeTest)
	env, err := l.Prepare()
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(env) != 0 {
		t.Fatalf("test launcher should export no env, got %v", env)
	}
	if err := l.Ready(context.Background()); err != nil {
		t.Fatalf("test launcher should always be ready, got %v", err)
	}
}

func TestLauncherForUnknownUsesAlwaysReady(t *testing.T) {
	if err := LauncherFor("telegramcustomer").Ready(context.Background()); err != nil {
		t.Fatalf("unknown/default launcher should be ready, got %v", err)
	}
}
