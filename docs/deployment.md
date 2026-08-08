# Deployment interface

## Process contract

- Listen on `HTTP_ADDR` (default `:8080`).
- Enforce bounded read/write/idle timeouts, request-body size, and connection concurrency using the non-secret `HTTP_*` settings in `.env.example`.
- Return liveness at `GET /healthz` and readiness at `GET /readyz`.
- Return build metadata at `GET /version`.
- Emit newline-delimited JSON logs to stdout.
- Stop accepting traffic after `SIGTERM`, mark readiness false, and allow `SHUTDOWN_TIMEOUT` (default `10s`) for graceful shutdown.
- Run as numeric user/group `65532:65532`, drop Linux capabilities and mount a bounded writable `emptyDir` at `/tmp` for FFmpeg workspaces.

## Container settings

Use a read-only root filesystem, drop all capabilities, and disallow privilege escalation. The service needs only an unprivileged TCP listener. Provide CPU/memory requests and limits in the deployment configuration.

Suggested probe paths are `/healthz` for liveness and `/readyz` for readiness. The initial readiness check has no external dependency; later integrations must add dependency-specific readiness without exposing credentials or internal error details.

All values in `.env.example` are non-secret. `LOCAL_LLM_URL` must target the
private OpenAI-compatible gateway and `LOCAL_LLM_MODEL` selects its model; the
mission flow remains operational when the endpoint is empty or unavailable.
Production requires MongoDB, the internal SeaweedFS S3 endpoint, a public
SeaweedFS presign endpoint and OIDC verification. Secrets `MONGO_URI`,
`S3_ACCESS_KEY`, `S3_SECRET_KEY`, and optional `LOCAL_LLM_API_KEY` must come
from Kubernetes Secret and must not enter `.env.example`, image layers,
command-line arguments, or logs. Non-secret runtime keys are:

- `APP_BASE_PATH=/dev/llm-api` for Development subpath routing.
- `S3_ENDPOINT=http://s3.store.svc.cluster.local:9000` for internal I/O.
- `S3_PRESIGN_ENDPOINT=https://<site>-s3.<domain>` (or
  `S3_PUBLIC_BASE_URL`) for browser-safe signed downloads.
- `OIDC_ISSUER` and `OIDC_AUDIENCE`; production fails closed without both.
- `ALLOW_TRUSTED_IDENTITY_HEADER=true` only for a controlled Alpha behind a
  gateway that strips caller-provided identity headers; production forbids it.
- `LOCAL_LLM_URL=http://local-ai-bridge.local-ai.svc.cluster.local:18082/v1`
  and `LOCAL_LLM_MODEL=qwen3.6-27b-q8_0-mtp-16k`.

The export endpoint is pollable: its first call returns `exportQueued`; later
calls refresh job state and return `exported` plus a fresh 15-minute signed URL
only after ffprobe validation and SeaweedFS upload. The leased MongoDB worker
reclaims interrupted jobs after restart. FFmpeg/ffprobe are installed in the
runtime image. Text/CTA overlays remain a follow-up worker enhancement; the
Mission Zero export currently normalizes, orders and concatenates the verified
three-shot vertical video without inventing product claims.

## Build toolchain

The repository-root `VERSION` file is the sole application-version authority. Container builds must pass its exact validated SemVer as `VERSION` and the full 40-character lowercase source commit as `VCS_REF`. The Dockerfile rejects missing, malformed, or mismatched metadata and records both values in the binary and OCI image labels. Deployments must reject an image when its labels or `GET /version` response differ from the D3 evidence for that version/SHA pair.

The source declares Go `1.26.0` language/module semantics and the patched `go1.26.5` toolchain. The container builder is `golang:1.26.5-alpine` pinned to OCI index digest `sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2`. Version selection is based on the [official Go release metadata](https://go.dev/dl/?mode=json); the digest is resolved from the registry for the [Docker Official Image for Go](https://hub.docker.com/_/golang). Updating either value requires rerunning tests, vet, and `govulncheck` without performing a local production or container build.
