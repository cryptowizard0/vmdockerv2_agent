package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveOpenclawStateDir(t *testing.T) {
	fakeHome := func() (string, error) { return "/user-home", nil }

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "state dir override wins",
			env: map[string]string{
				"OPENCLAW_STATE_DIR": "/sandbox/state",
				"OPENCLAW_HOME":      "/ignored-home",
				"HOME":               "/ignored",
			},
			want: "/sandbox/state",
		},
		{
			name: "openclaw home derives state dir",
			env: map[string]string{
				"OPENCLAW_HOME": "/sandbox/home",
			},
			want: "/sandbox/home/.openclaw",
		},
		{
			name: "home derives state dir",
			env: map[string]string{
				"HOME": "/sandbox/user",
			},
			want: "/sandbox/user/.openclaw",
		},
		{
			name: "slash home falls back to user home dir",
			env: map[string]string{
				"HOME": "/",
			},
			want: "/user-home/.openclaw",
		},
		{
			name: "missing home falls back to tmp",
			env:  map[string]string{},
			want: "/user-home/.openclaw",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveOpenclawStateDir(func(key string) string {
				return tt.env[key]
			}, fakeHome)
			if got != tt.want {
				t.Fatalf("ResolveOpenclawStateDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveOpenclawStateDirFallbackToTmp(t *testing.T) {
	got := ResolveOpenclawStateDir(func(string) string { return "" }, func() (string, error) {
		return "/", nil
	})
	if got != "/tmp/.openclaw" {
		t.Fatalf("ResolveOpenclawStateDir() = %q, want /tmp/.openclaw", got)
	}
}

func TestPrepareOpenclawRuntimeMaterializesConfigOnce(t *testing.T) {
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, "template.json")
	if err := os.WriteFile(templatePath, []byte("{\n  \"gateway\": {\n    \"mode\": \"local\"\n  },\n  \"tools\": {\n    \"sessions\": {\n      \"visibility\": \"all\"\n    }\n  }\n}\n"), 0o644); err != nil {
		t.Fatalf("write template failed: %v", err)
	}

	configPath := filepath.Join(tempDir, "state", "openclaw.json")
	env := map[string]string{
		"OPENCLAW_STATE_DIR":            filepath.Join(tempDir, "state"),
		"OPENCLAW_CONFIG_TEMPLATE_PATH": templatePath,
		"OPENCLAW_CONFIG_PATH":          configPath,
		"OPENCLAW_AGENT_WORKSPACE":      filepath.Join(tempDir, "state", ".workspace"),
		"OPENCLAW_DEFAULT_PROVIDER":     "zen",
		"OPENCLAW_DEFAULT_MODEL":        "kimi-coding/plan",
		"OPENCLAW_GATEWAY_MODE":         "proxy",
		"HOME":                          filepath.Join(tempDir, "home"),
		"TMPDIR":                        filepath.Join(tempDir, "tmp"),
		"XDG_CONFIG_HOME":               filepath.Join(tempDir, "xdg", "config"),
		"XDG_CACHE_HOME":                filepath.Join(tempDir, "xdg", "cache"),
		"XDG_STATE_HOME":                filepath.Join(tempDir, "xdg", "state"),
	}

	paths, err := PrepareOpenclawRuntime(func(key string) string { return env[key] }, nil)
	if err != nil {
		t.Fatalf("PrepareOpenclawRuntime() failed: %v", err)
	}

	if paths.StateDir != env["OPENCLAW_STATE_DIR"] {
		t.Fatalf("state dir = %q, want %q", paths.StateDir, env["OPENCLAW_STATE_DIR"])
	}
	if paths.ConfigPath != configPath {
		t.Fatalf("config path = %q, want %q", paths.ConfigPath, configPath)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	text := string(raw)
	for _, snippet := range []string{
		"\"mode\": \"proxy\"",
		"\"primary\": \"zen/plan\"",
		"\"workspace\": \"" + env["OPENCLAW_AGENT_WORKSPACE"] + "\"",
		"\"workspaceOnly\": true",
		"\"file\": \"" + filepath.Join(env["OPENCLAW_STATE_DIR"], "logs", "openclaw.log") + "\"",
		"\"visibility\": \"all\"",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected config to contain %q, got: %s", snippet, text)
		}
	}

	if err := os.WriteFile(configPath, []byte("{\"gateway\":{\"mode\":\"custom\"},\"tools\":{\"sessions\":{\"visibility\":\"all\"}}}\n"), 0o600); err != nil {
		t.Fatalf("rewrite config failed: %v", err)
	}

	if _, err := PrepareOpenclawRuntime(func(key string) string { return env[key] }, nil); err != nil {
		t.Fatalf("second PrepareOpenclawRuntime() failed: %v", err)
	}

	raw, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after second prepare failed: %v", err)
	}
	text = string(raw)
	if !strings.Contains(text, "\"mode\": \"custom\"") {
		t.Fatalf("expected custom gateway mode to be preserved: %s", text)
	}
	if !strings.Contains(text, "\"primary\": \"zen/plan\"") {
		t.Fatalf("expected managed primary model to persist: %s", text)
	}
	if !strings.Contains(text, "\"workspace\": \""+env["OPENCLAW_AGENT_WORKSPACE"]+"\"") {
		t.Fatalf("expected managed workspace to persist: %s", text)
	}
	if !strings.Contains(text, "\"workspaceOnly\": true") {
		t.Fatalf("expected managed workspaceOnly to persist: %s", text)
	}
	if !strings.Contains(text, "\"file\": \""+filepath.Join(env["OPENCLAW_STATE_DIR"], "logs", "openclaw.log")+"\"") {
		t.Fatalf("expected managed log file to persist: %s", text)
	}
	if !strings.Contains(text, "\"visibility\": \"all\"") {
		t.Fatalf("expected unrelated config to be preserved: %s", text)
	}

	sessionsPath := filepath.Join(env["OPENCLAW_STATE_DIR"], "agents", "main", "sessions", "sessions.json")
	if _, err := os.Stat(sessionsPath); err != nil {
		t.Fatalf("sessions store missing: %v", err)
	}

	expectedLogPath := filepath.Join(env["OPENCLAW_STATE_DIR"], "logs", "openclaw-gateway.log")
	if paths.GatewayLogPath != expectedLogPath {
		t.Fatalf("gateway log path = %q, want %q", paths.GatewayLogPath, expectedLogPath)
	}
	if _, err := os.Stat(paths.LogDir); err != nil {
		t.Fatalf("gateway log dir missing: %v", err)
	}

	authStorePath := filepath.Join(env["OPENCLAW_STATE_DIR"], "agents", "main", "agent")
	if _, err := os.Stat(authStorePath); err != nil {
		t.Fatalf("auth store dir missing: %v", err)
	}

	if paths.AgentWorkspace != env["OPENCLAW_AGENT_WORKSPACE"] {
		t.Fatalf("agent workspace = %q, want %q", paths.AgentWorkspace, env["OPENCLAW_AGENT_WORKSPACE"])
	}
	for _, dir := range []string{
		env["OPENCLAW_AGENT_WORKSPACE"],
		env["HOME"],
		env["TMPDIR"],
		env["XDG_CONFIG_HOME"],
		env["XDG_CACHE_HOME"],
		env["XDG_STATE_HOME"],
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected runtime dir %q: %v", dir, err)
		}
		if got := info.Mode().Perm(); got != 0o777 {
			t.Fatalf("runtime dir %q perm = %o, want 777", dir, got)
		}
	}
}

func TestResolveOpenclawModelDefaults(t *testing.T) {
	defaults := ResolveOpenclawModelDefaults(func(key string) string {
		switch key {
		case "OPENCLAW_DEFAULT_PROVIDER":
			return "Zen"
		case "OPENCLAW_DEFAULT_MODEL":
			return "kimi-coding/plan"
		default:
			return ""
		}
	})

	if defaults.Provider != "zen" {
		t.Fatalf("provider = %q, want zen", defaults.Provider)
	}
	if defaults.Model != "kimi-coding/plan" {
		t.Fatalf("model = %q, want kimi-coding/plan", defaults.Model)
	}
}

func TestNormalizeManagedPrimaryModel(t *testing.T) {
	tests := []struct {
		name     string
		defaults OpenclawModelDefaults
		want     string
	}{
		{
			name: "provider prefixes plain model",
			defaults: OpenclawModelDefaults{
				Provider: "zen",
				Model:    "plan",
			},
			want: "zen/plan",
		},
		{
			name: "provider rewrites prefixed model",
			defaults: OpenclawModelDefaults{
				Provider: "zen",
				Model:    "kimi-coding/plan",
			},
			want: "zen/plan",
		},
		{
			name: "missing provider preserves model",
			defaults: OpenclawModelDefaults{
				Model: "kimi-coding/k2p5",
			},
			want: "kimi-coding/k2p5",
		},
		{
			name: "missing model yields empty",
			defaults: OpenclawModelDefaults{
				Provider: "zen",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeManagedPrimaryModel(tt.defaults)
			if got != tt.want {
				t.Fatalf("normalizeManagedPrimaryModel() = %q, want %q", got, tt.want)
			}
		})
	}
}
