# KWANNI API

Go/Fiber backend for the KWANNI guided affiliate starter. Version `0.2.0`
adds the first end-to-end Mission Zero slice:

`manual product → mission → 3-shot capture → draft → export → mark posted`

The current media step creates a deterministic edit plan and export target. An
FFmpeg worker, MongoDB adapter, and SeaweedFS S3 production adapter remain
explicit deployment work; see [Mission Zero Phase 1](docs/mission-zero-phase1.md).

## Local checks

```sh
gofmt -w cmd internal
go test ./...
go vet ./...
./scripts/verify-version.sh
./scripts/verify-version_test.sh
```

Local container builds are intentionally omitted. The shared CI platform builds the digest-pinned `Dockerfile`; see [the CI contract](docs/ci-contract.md) and [deployment interface](docs/deployment.md).

Remote CI is not wired. The restricted/private [klynx-cluster-deploy PR #131](https://github.com/pointitconsulting/klynx-cluster-deploy/pull/131) is a D1 security prerequisite only; it does not implement D3. D3 onboarding is later work and remains unimplemented. Local validation must not be presented as remote CI evidence.

## Version contract

[`VERSION`](VERSION) is the authoritative application version and contains one exact [Semantic Versioning 2.0.0](https://semver.org/) value. D3 must read this file; it must not infer a version from a branch, tag, package, or mutable CI setting. `./scripts/verify-version.sh` validates the file and prints the version for automation.

Every image build must pass that exact value as `VERSION` and the checked-out 40-character lowercase commit SHA as `VCS_REF`. The build fails if either value is absent, malformed, or if `VERSION` differs from the file. The same values are embedded in `GET /version` and the OCI image labels `org.opencontainers.image.version` and `org.opencontainers.image.revision`.

## Runtime

Copy `.env.example` values into your process environment and run `go run ./cmd/api`. The service exposes:

- `GET /healthz`
- `GET /readyz`
- `GET /version`
- `POST /v1/missions`
- `GET /v1/missions/{id}`
- `PUT /v1/missions/{id}/assets/{shot}`
- `POST /v1/missions/{id}/draft`
- `POST /v1/missions/{id}/export`
- `POST /v1/missions/{id}/posted`

The local LLM is optional. If it is unavailable or returns invalid structured
output, the API uses a deterministic Thai caption so First Mission remains
usable. No endpoint promises income or sales.

Configuration is restricted to the non-secret settings documented in `.env.example`. JSON request logs are written to stdout and include a validated or generated `X-Request-ID`.

## Branch flow

`feat/*` → `develop` → `main`

## License

Copyright (c) 2026. All rights reserved. Public visibility does not grant an open-source license or determine ownership; see [NOTICE](NOTICE).
