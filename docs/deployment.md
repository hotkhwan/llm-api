# Deployment interface

## Process contract

- Listen on `HTTP_ADDR` (default `:8080`).
- Enforce bounded read/write/idle timeouts, request-body size, and connection concurrency using the non-secret `HTTP_*` settings in `.env.example`.
- Return liveness at `GET /healthz` and readiness at `GET /readyz`.
- Return build metadata at `GET /version`.
- Emit newline-delimited JSON logs to stdout.
- Stop accepting traffic after `SIGTERM`, mark readiness false, and allow `SHUTDOWN_TIMEOUT` (default `10s`) for graceful shutdown.
- Run as numeric user/group `65532:65532`; no writable filesystem or Linux capabilities are required.

## Container settings

Use a read-only root filesystem, drop all capabilities, and disallow privilege escalation. The service needs only an unprivileged TCP listener. Provide CPU/memory requests and limits in the deployment configuration.

Suggested probe paths are `/healthz` for liveness and `/readyz` for readiness. The initial readiness check has no external dependency; later integrations must add dependency-specific readiness without exposing credentials or internal error details.

All values in `.env.example` are non-secret. Future secret inputs require an explicit secret-provider contract and must not be added to `.env.example`, image layers, command-line arguments, or logs.

## Build toolchain

The source declares Go `1.26.0` language/module semantics and the patched `go1.26.5` toolchain. The container builder is `golang:1.26.5-alpine` pinned to OCI index digest `sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2`. Version selection is based on the [official Go release metadata](https://go.dev/dl/?mode=json); the digest is resolved from the registry for the [Docker Official Image for Go](https://hub.docker.com/_/golang). Updating either value requires rerunning tests, vet, and `govulncheck` without performing a local production or container build.
