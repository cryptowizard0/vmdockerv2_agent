# vmdockerv2_agent

`vmdockerv2_agent` is the platform adapter that runs as PID 1 inside every
Module image built by [vmdockerv2](../vmdockerv2). It exposes the HyMatrix VMM
HTTP protocol, starts the Module author's command, and bridges VMM requests to
the selected agent runtime.

This repository produces only the Linux adapter binary. Image construction,
Module signing, container lifecycle management, and per-process workspace
isolation belong to `vmdockerv2`.

## Responsibilities

The adapter:

- serves `POST /vmm/health`, `/vmm/spawn`, `/vmm/apply`,
  `/vmm/checkpoint`, and `/vmm/restore` on port `8080`;
- selects an implementation through `RUNTIME_TYPE`;
- performs runtime-specific preparation before the Module command starts;
- launches the image `CMD` in its own process group and captures its output;
- gates `/vmm/health` on runtime-specific readiness;
- reaps orphaned child processes and forwards `SIGTERM` to the command's
  process group.

The runtime instance is created lazily by `/vmm/spawn` or `/vmm/restore`. The
HTTP server and the Module command start before that instance exists.

## How it fits into vmdockerv2

```text
vmdockerv2_agent                         vmdockerv2
----------------                         ----------
scripts/build.sh
  -> build/vmdocker-agent  -- inject --> Module image
                                            ENTRYPOINT ["/usr/local/bin/vmdocker-agent"]
                                            CMD        [<module command>]
                                                        |
                                                        v
                                            running Module container
                                            /vmm/* on port 8080
```

`RUNTIME_TYPE` is not baked into the image. `vmdockerv2` passes it at Spawn
time through the `Container-Env-RUNTIME_TYPE` tag.

## Requirements

- Go 1.25 or later
- Access to the private GitHub dependencies listed in `go.mod`
- A Linux target environment for the resulting binary

The build script configures `GOPRIVATE` for the required GitHub organizations.
Direct `go build` or `go test` commands on a cold module cache may require:

```bash
export GOPRIVATE=github.com/hymatrix,github.com/xingj404-lab
```

GitHub SSH or token access must already be configured.

## Build and test

Run the test suite:

```bash
go test ./...
```

Build a static Linux binary for the host architecture:

```bash
scripts/build.sh
```

Build for a specific target architecture:

```bash
scripts/build.sh amd64
scripts/build.sh arm64
```

All build commands write `build/vmdocker-agent`.

## Local smoke test

The `test` runtime has no external engine dependency and is the simplest way to
start the adapter locally:

```bash
RUNTIME_TYPE=test go run .
```

In another terminal, verify the adapter. All VMM endpoints, including health,
use `POST`:

```bash
curl -fsS -X POST http://127.0.0.1:8080/vmm/health
```

Expected response:

```json
{"status":"ok"}
```

Press `Ctrl-C` to stop the process. Local `/vmm/spawn` and `/vmm/apply`
requests use HyMatrix schema types; normal end-to-end calls are issued by a
`vmdockerv2` node.

## Use the binary in vmdockerv2

Build the adapter for the same architecture as the Module's base image, then
pass its absolute path to the Module builder:

```bash
cd ../vmdockerv2_agent
scripts/build.sh amd64
export VMDOCKER_AGENT_BIN="$PWD/build/vmdocker-agent"

cd ../vmdockerv2
go run ./cmd/module \
  -profile /absolute/path/to/profile.toml \
  -agent-bin "$VMDOCKER_AGENT_BIN"
```

The generated image keeps the adapter as its fixed `ENTRYPOINT`. An optional
`[dockerfile].CMD` in `profile.toml` becomes the Module command and is passed to
the adapter as arguments. For example:

```toml
[dockerfile]
FROM = "docker/sandbox-templates:shell"
bin = "bin"
CMD = ["my-agent", "--serve"]
```

At container start, the effective command is equivalent to:

```text
/usr/local/bin/vmdocker-agent my-agent --serve
```

If the image has no `CMD`, the adapter still serves `/vmm/*`. For compatibility
with older images, it runs `VMDOCKER_USER_STARTUP_HOOK` when that file exists.

## Startup and request lifecycle

1. Read `RUNTIME_TYPE` and choose a launcher.
2. Prepare runtime state and export any generated environment variables.
3. Start the image `CMD`, or the legacy startup hook when no command was
   supplied.
4. Start the VMM HTTP API on `0.0.0.0:8080`.
5. Report runtime-engine readiness from `/vmm/health`.
6. Create one runtime instance on `/vmm/spawn`, then accept `/vmm/apply` and
   `/vmm/checkpoint` calls. `/vmm/restore` creates an instance from saved state
   instead of spawning a new one.
7. On `SIGINT` or `SIGTERM`, forward `SIGTERM` to the Module command's process
   group and shut down the HTTP server.

The HTTP API has the following state rules:

| Endpoint | Purpose | Runtime state requirement |
|---|---|---|
| `POST /vmm/health` | Check engine readiness | Independent of Spawn state |
| `POST /vmm/spawn` | Create a runtime instance | Must not already exist |
| `POST /vmm/apply` | Execute a runtime request | Must already exist |
| `POST /vmm/checkpoint` | Serialize runtime state | Must already exist |
| `POST /vmm/restore` | Create a runtime from checkpoint state | Must not already exist |

## Supported runtimes

| `RUNTIME_TYPE` | Runtime integration | `/vmm/health` readiness |
|---|---|---|
| `test` | In-memory Echo/Ping implementation; default | Always ready |
| `openclaw` | OpenClaw gateway | Gateway health request succeeds |
| `claude` | Claude Code CLI | `claude` is available on `PATH` |
| `hermes` | Hermes Agent Gateway API | Gateway health request succeeds |
| `telegramcustomer` | Telegram customer runtime backed by Claude | Always ready; initialization happens on Spawn |

An unsupported value can pass the always-ready launcher check, but
`/vmm/spawn` or `/vmm/restore` will reject it.

### Common adapter configuration

| Variable | Default | Description |
|---|---|---|
| `RUNTIME_TYPE` | `test` | Runtime selected at boot and Spawn |
| `VMDOCKER_USER_STARTUP_LOG` | `/tmp/vmdocker-user-startup.log` | Combined stdout/stderr for the Module command |
| `VMDOCKER_USER_STARTUP_HOOK` | `/usr/local/lib/vmdocker-agent/user-startup.sh` | Legacy fallback used only when no command was supplied |
| `VMDOCKER_AGENT_WORKSPACE` | empty | Preferred working directory for Claude-based runtimes |
| `VMDOCKER_RUNTIME_WORKSPACE` | empty | Secondary workspace fallback; the process working directory is used when no workspace variable is set |

### OpenClaw configuration

OpenClaw preparation creates its state layout, materializes a config file from
a template, applies managed workspace/model/logging defaults, and then starts
the image command. The base image must provide the template unless its path is
overridden.

| Variable | Default | Description |
|---|---|---|
| `OPENCLAW_GATEWAY_URL` | `http://127.0.0.1:18789` | Gateway base URL |
| `OPENCLAW_GATEWAY_TOKEN` | empty | Bearer token sent to the gateway |
| `OPENCLAW_TIMEOUT_MS` | `30000` | Gateway request timeout |
| `OPENCLAW_CONFIG_TEMPLATE_PATH` | `/app/openclaw.default.json` | Source config template |
| `OPENCLAW_CONFIG_PATH` | `<state-dir>/openclaw.json` | Materialized config path |
| `OPENCLAW_STATE_DIR` | derived from `OPENCLAW_HOME` or `HOME` | Persistent OpenClaw state directory |
| `OPENCLAW_AGENT_WORKSPACE` | empty | Managed agent workspace; also restricts filesystem tools to that workspace |
| `OPENCLAW_DEFAULT_PROVIDER` | empty | Provider used to normalize the managed primary model |
| `OPENCLAW_DEFAULT_MODEL` | empty | Managed primary model |
| `OPENCLAW_GATEWAY_MODE` | empty | Optional gateway mode written to the materialized config |
| `OPENCLAW_SESSION_KEY` | `main` | Fallback session key when session creation is unavailable |

Endpoint and tool names can also be overridden with the
`OPENCLAW_ENDPOINT_*` and `OPENCLAW_TOOL_*` variables defined in
`runtime/openclaw`.

### Claude configuration

The `claude` runtime invokes Claude Code in print mode and persists its session
ID in checkpoints.

| Variable | Default | Description |
|---|---|---|
| `CLAUDE_CODE_BIN` | `claude` | Claude Code executable |
| `CLAUDE_CODE_TIMEOUT_MS` | `600000` | Per-Apply timeout |
| `CLAUDE_CODE_FLAGS` | empty | Additional shell-style CLI arguments |
| `ANTHROPIC_API_KEY` | empty | API key passed to Claude Code |
| `ANTHROPIC_BASE_URL` | empty | Optional Anthropic-compatible base URL |
| `ANTHROPIC_MODEL` | empty | Preferred model; falls back to `CLAUDE_MODEL` or the Spawn tags |

### Telegram customer configuration

`telegramcustomer` shares the Claude CLI and workspace variables. It also
requires `BOT_TOKEN` or a `botToken` Spawn tag. Its Apply operation is currently
not implemented; the runtime starts the Telegram integration during Spawn and
supports checkpoint/restore state.

### Hermes Gateway configuration

The `hermes` runtime does not start a Hermes CLI process. The Module `CMD`
should run an author-provided startup program that writes Hermes configuration,
plugins, and skills, enables the Hermes API server, and then starts the gateway
in the foreground. The adapter waits for `GET /health`. Hermes serves clients
directly; `/vmm/apply` intentionally returns an unsupported-operation error.
Checkpoint/restore uses a stateless compatibility token required by the shared
VMM runtime interface. VMDocker checkpoints preserve the complete mounted HOME
workspace; Export/Respawn preserves Hermes sessions and memory through the
allowlisted `~/.hermes/state.db*` files.

| Variable | Default | Description |
|---|---|---|
| `HERMES_GATEWAY_URL` | `http://127.0.0.1:8642` | Hermes API server base URL |
| `HERMES_GATEWAY_TOKEN` | `API_SERVER_KEY` | Bearer token sent to the API server |
| `HERMES_GATEWAY_TIMEOUT_MS` | `600000` | Gateway health-client timeout |

For the Hermes image used by this project, a profile can start with:

```toml
[dockerfile]
FROM = "sandytest456/hermes-agent:linux-full"
bin = "bin"
CMD = ["start-hermes"]

[vmdocker]
public = [
  "~/skills/*",
  "~/persona/*",
  "~/.hermes/plugin/*",
  "~/.hermes/state.db*",
]
```

Place the executable startup program at `bin/start-hermes`. It must set at
least `API_SERVER_ENABLED=true` and `API_SERVER_KEY`, then keep the gateway in
the foreground, normally by replacing itself with `hermes gateway run`. Pass
the same key to the adapter as `HERMES_GATEWAY_TOKEN`, or let it inherit the
shared `API_SERVER_KEY` environment variable.

`~/.hermes/state.db*` includes the SQLite database and any WAL/SHM companions
needed to resolve a checkpointed session after Export and a fresh Spawn. It can
contain conversation history, so include it only when session continuity is
intended. Do not export `~/.hermes/.env` or `config.yaml`; recreate configuration
and inject credentials from the startup program instead.

## Repository layout

```text
.
├── main.go                 Process entrypoint and fixed port
├── common/                 Logging and HTTP middleware
├── runtime/                Runtime selection and implementations
│   ├── claudecode/
│   ├── hermes/
│   ├── openclaw/
│   ├── telegramcustomer/
│   └── testrt/
├── server/                 VMM routes and server lifecycle
├── supervisor/             Child process supervision and signal handling
├── utils/                  OpenClaw bootstrap helpers
└── scripts/build.sh        Static Linux build script
```

## Troubleshooting

### Health returns `503 not_ready`

- For `openclaw`, verify the image command started the gateway and that
  `OPENCLAW_GATEWAY_URL` is reachable from inside the container.
- For `claude`, verify the literal `claude` command is available on `PATH`.
  The health launcher checks that command even when `CLAUDE_CODE_BIN` is set
  for Apply calls.
- For `hermes`, verify the Module startup command enabled the API server and
  started `hermes gateway run`; then check `HERMES_GATEWAY_URL`,
  `HERMES_GATEWAY_TOKEN`, and `VMDOCKER_USER_STARTUP_LOG`.
- Check `VMDOCKER_USER_STARTUP_LOG` for command output.

### OpenClaw fails before the API starts

OpenClaw preparation runs before the HTTP listener starts. Confirm that
`OPENCLAW_CONFIG_TEMPLATE_PATH` exists and that the state, config, log, and
workspace directories are writable.

### Spawn reports `runtime type not supported`

Use one of `test`, `openclaw`, `claude`, `hermes`, or `telegramcustomer`. The value must
be passed into the container at Spawn time, normally with
`Container-Env-RUNTIME_TYPE`.

### The Module command did not start

Confirm that the Module image has the expected `CMD`, then inspect
`/tmp/vmdocker-user-startup.log` (or the path configured by
`VMDOCKER_USER_STARTUP_LOG`). A Module without `CMD` intentionally runs only
the adapter unless the legacy startup hook exists.

## License

See [LICENSE](LICENSE).
