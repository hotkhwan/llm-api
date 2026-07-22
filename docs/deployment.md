# Deployment interface

## Process contract

- Listen on `HTTP_ADDR` (default `:8080`).
- Return liveness at `GET /healthz` and readiness at `GET /readyz`.
- Return build metadata at `GET /version`.
- Emit newline-delimited JSON logs to stdout.
- Stop accepting traffic after `SIGTERM`, mark readiness false, and allow `SHUTDOWN_TIMEOUT` (default `10s`) for graceful shutdown.
- Run as numeric user/group `65532:65532`; no writable filesystem or Linux capabilities are required.

## Container settings

Use a read-only root filesystem, drop all capabilities, and disallow privilege escalation. The service needs only an unprivileged TCP listener. Provide CPU/memory requests and limits in the deployment configuration.

Suggested probe paths are `/healthz` for liveness and `/readyz` for readiness. The initial readiness check has no external dependency; later integrations must add dependency-specific readiness without exposing credentials or internal error details.

All values in `.env.example` are non-secret. Future secret inputs require an explicit secret-provider contract and must not be added to `.env.example`, image layers, command-line arguments, or logs.
