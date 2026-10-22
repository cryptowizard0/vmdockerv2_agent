// runtime/launcher.go
package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/cryptowizard0/vmdockerv2_agent/runtime/openclaw"
	"github.com/cryptowizard0/vmdockerv2_agent/utils"
)

// Launcher is the runtime-type-specific container-boot behavior: preparation
// done before start.sh runs, and the readiness probe /vmm/health gates on. It
// is independent of IRuntime (the spawn instance created lazily on /vmm/spawn).
type Launcher interface {
	// Prepare runs pre-start.sh setup and returns "KEY=value" assignments to
	// export before spawning start.sh.
	Prepare() ([]string, error)
	// Ready returns nil when the runtime engine is ready to serve.
	Ready(ctx context.Context) error
}

// CurrentRuntimeType returns the configured RUNTIME_TYPE, defaulting to test.
func CurrentRuntimeType() string {
	if t := os.Getenv("RUNTIME_TYPE"); t != "" {
		return t
	}
	return RuntimeTypeTest
}

// LauncherFor maps a runtime type to its Launcher. Unknown types (including
// telegramcustomer) use the always-ready launcher: no engine prep, no gating.
func LauncherFor(runtimeType string) Launcher {
	switch runtimeType {
	case RuntimeTypeOpenclaw:
		return openclawLauncher{}
	case RuntimeTypeClaude:
		return claudeLauncher{}
	case RuntimeTypeHermes:
		return hermesLauncher{}
	default:
		return alwaysReadyLauncher{}
	}
}

type alwaysReadyLauncher struct{}

func (alwaysReadyLauncher) Prepare() ([]string, error)  { return nil, nil }
func (alwaysReadyLauncher) Ready(context.Context) error { return nil }

type claudeLauncher struct{}

func (claudeLauncher) Prepare() ([]string, error) { return nil, nil }

func (claudeLauncher) Ready(context.Context) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude CLI not on PATH: %w", err)
	}
	return nil
}

type hermesLauncher struct{}

func (hermesLauncher) Prepare() ([]string, error) { return nil, nil }

// Hermes is started by Apply(Action=Start-Agent), after /vmm/spawn. Initial
// readiness means the adapter is ready to accept Spawn.
func (hermesLauncher) Ready(context.Context) error { return nil }

type openclawLauncher struct{}

func (openclawLauncher) Prepare() ([]string, error) {
	paths, err := utils.PrepareOpenclawRuntime(os.Getenv, os.UserHomeDir)
	if err != nil {
		return nil, fmt.Errorf("prepare openclaw runtime: %w", err)
	}
	return []string{
		"OPENCLAW_STATE_DIR=" + paths.StateDir,
		"OPENCLAW_CONFIG_PATH=" + paths.ConfigPath,
		"OPENCLAW_GATEWAY_LOG_PATH=" + paths.GatewayLogPath,
	}, nil
}

func (openclawLauncher) Ready(ctx context.Context) error {
	client := openclaw.NewHTTPGatewayClient(openclaw.LoadConfigFromEnv())
	if err := client.Init(ctx); err != nil {
		return fmt.Errorf("openclaw gateway not ready: %w", err)
	}
	return nil
}
