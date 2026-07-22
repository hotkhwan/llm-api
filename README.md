# affiliate-api

Minimal Go/Fiber API service scaffold. This repository currently contains platform foundations only; application behavior will be introduced through separately reviewed contracts.

## Local checks

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
```

Local container builds are intentionally omitted. The shared CI platform builds the digest-pinned `Dockerfile`; see [the CI contract](docs/ci-contract.md) and [deployment interface](docs/deployment.md).

## Runtime

Copy `.env.example` values into your process environment and run `go run ./cmd/api`. The service exposes:

- `GET /healthz`
- `GET /readyz`
- `GET /version`

Configuration is restricted to the non-secret settings documented in `.env.example`. JSON request logs are written to stdout and include a validated or generated `X-Request-ID`.

## Branch flow

`feat/*` → `develop` → `main`
