# affiliate-api

Minimal Go/Fiber API service scaffold. This repository currently contains platform foundations only; application behavior will be introduced through separately reviewed contracts.

## Local checks

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
```

Local container builds are intentionally omitted. The shared CI platform builds the digest-pinned `Dockerfile`; see [the CI contract](docs/ci-contract.md) and [deployment interface](docs/deployment.md).

Remote CI is not wired. The restricted/private [klynx-cluster-deploy PR #131](https://github.com/pointitconsulting/klynx-cluster-deploy/pull/131) is a D1 security prerequisite only; it does not implement D3. D3 onboarding is later work and remains unimplemented. Local validation must not be presented as remote CI evidence.

## Runtime

Copy `.env.example` values into your process environment and run `go run ./cmd/api`. The service exposes:

- `GET /healthz`
- `GET /readyz`
- `GET /version`

Configuration is restricted to the non-secret settings documented in `.env.example`. JSON request logs are written to stdout and include a validated or generated `X-Request-ID`.

## Branch flow

`feat/*` → `develop` → `main`

## License

Copyright (c) 2026. All rights reserved. Public visibility does not grant an open-source license or determine ownership; see [NOTICE](NOTICE).
