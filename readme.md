# vmdockerv2_agent

The `/vmm` **adapter entrypoint binary** that runs as PID 1 inside every module
image built by [vmdockerv2](../vmdockerv2). It:

- serves the platform `/vmm/*` API (spawn / apply / checkpoint / restore) on `:8080`;
- selects a runtime backend from `RUNTIME_TYPE` (openclaw / claude / telegramcustomer / test);
- runs runtime-type prep, spawns the module author's `start.sh`, and gates
  `/vmm/health` on runtime-type readiness;
- acts as PID 1 init (reaps zombies, forwards `SIGTERM`).

## Scope

This repo produces **only** the adapter binary. Base images and module/image
construction (profile → Dockerfile → `docker build` → module) live in
**vmdockerv2** (`vmdocker/modulebuild`). vmdockerv2_agent does not build images.

## Build

    scripts/build.sh            # linux, host arch
    scripts/build.sh amd64      # linux/amd64

Produces `build/vmdocker-agent`. vmdockerv2 consumes it at module-build time via
`VMDOCKER_AGENT_BIN=/path/to/build/vmdocker-agent`, copying it in as the image
ENTRYPOINT.
