# Repository Guidelines

## Project Structure & Module Organization
This repository is a Go service that exposes a VMM-compatible HTTP API.

- `main.go`: process entrypoint (`server.New(8080).Run()`).
- `server/`: API routes and server lifecycle (`/vmm/health`, `/vmm/spawn`, `/vmm/apply`).
- `runtime/`: runtime selection and implementations.
- `runtime/testrt/`: in-memory test runtime.
- `runtime/openclaw/`: Openclaw gateway-backed runtime (`openclaw.go`: core logic, `tools.go`: tool definitions, `setup.go`: initialization, `gateway.go`: HTTP client).
- `common/`: shared logging and middleware.
- `utils/`: helper utilities.
- `scripts/build.sh`: builds the adapter entrypoint binary (`build/vmdocker-agent`, linux).

This repository produces **only** the `/vmm` adapter entrypoint binary. Base images and module/image construction live in **vmdockerv2** (`vmdocker/modulebuild`); vmdockerv2_agent does not build images. vmdockerv2 injects the adapter binary at module-build time via `VMDOCKER_AGENT_BIN`.

Keep new runtime implementations under `runtime/` and add package-local tests alongside code.

## Build, Test, and Development Commands
- `scripts/build.sh [GOARCH]`: build the adapter entrypoint binary → `build/vmdocker-agent` (linux; arch defaults to the host). This is the artifact vmdockerv2 consumes via `VMDOCKER_AGENT_BIN`.
- `go run main.go`: run the API locally on port `8080`.
- `go test ./...`: run all Go tests.
- `go test -v -cover ./...`: verbose tests with coverage.

Dependencies `github.com/hymatrix/*` and `github.com/xingj404-lab/claude-gw` are fetched direct over github (not the public proxy). `scripts/build.sh` sets `GOPRIVATE` for you; for bare `go build`/`go test` on a cold module cache, first `export GOPRIVATE=github.com/hymatrix,github.com/xingj404-lab` (github SSH/token access required).

## Coding Style & Naming Conventions
Use standard Go formatting and idioms:

- Format with `gofmt -w <file>` before committing.
- Package names are lowercase (`openclaw`, `testrt`).
- Exported symbols use PascalCase; internal helpers use camelCase.
- Test files use `*_test.go`, with `TestXxx` and clear `t.Fatalf(...)` messages.

Environment-driven config uses explicit names (for example `RUNTIME_TYPE`, `OPENCLAW_GATEWAY_URL`, `OPENCLAW_TIMEOUT_MS`).

## Testing Guidelines
Primary framework is Go’s built-in `testing` package with `httptest` for API/runtime behavior.

- Add unit tests next to changed package code.
- Prefer table-style or subtests (`t.Run(...)`) for action variants.
- Cover success and failure paths for JSON binding, runtime init, and gateway errors.

## Commit & Pull Request Guidelines
Recent commits use short, imperative summaries (for example: `Update vmdocker dependency to v0.0.2`).

- Commit message format: `Verb + scope/outcome` in one concise line.
- PRs should include: purpose, key behavior changes, env var impacts, and test evidence (command + result).
- If API behavior changes, include example request/response for `/vmm/*` endpoints.
