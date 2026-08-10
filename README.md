# KWANNI API

Go/Fiber backend for the KWANNI guided affiliate starter. Version `0.5.0`
implements the first durable end-to-end Mission Zero slice:

`product facts → one exact reference image → Qwen CPS → Veo/Seedance → Product Fidelity VLM+OCR → ShotVL → targeted Qwen shot repair → FFmpeg → SeaweedFS → download → mark posted → outcome`

MongoDB, SeaweedFS S3, a restart-safe leased FFmpeg worker, OIDC ownership,
Qwen role planning and Canonical Production Spec v1 are included; see
[Mission Zero backend](docs/mission-zero-phase1.md).
Manual-upload MZ-3 ShotVL review remains advisory. Cloud-generated First Post
uses a stricter gate: the exact original image is attached to every provider
request, a general VLM performs product comparison and OCR, ShotVL checks
cinematography, and Qwen may revise only the failed shot (maximum two
revisions). The final 1080x1920 MP4 is never assembled unless all three shots
pass both gates.

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
- `PUT /v1/missions/{id}/product-references/{index}`
- `POST /v1/missions/{id}/draft`
- `POST /v1/missions/{id}/generate-video`
- `POST /v1/missions/{id}/export`
- `POST /v1/missions/{id}/posted`
- `PUT /v1/missions/{id}/outcome`
- `POST /v1/missions/{id}/visual-qc`
- `PUT /v1/missions/{id}/visual-qc/override`

The local LLM is optional. If it is unavailable or returns invalid structured
output, the API uses a deterministic Thai plan so First Mission remains usable.
No endpoint promises income or sales. Production verifies bearer JWTs with the
configured OIDC issuer/audience; the trusted identity header is allowed only
when explicitly enabled for a controlled development/Alpha environment.

Configuration is restricted to the non-secret settings documented in `.env.example`. JSON request logs are written to stdout and include a validated or generated `X-Request-ID`.

Provider credentials are Kubernetes Secret values only. Veo uses the current
Gemini Veo 3.1 long-running REST contract and sends the product as an immutable
`referenceImages` asset. Seedance uses Ark content-generation tasks and sends
the same bytes as a `reference_image`. Provider output is copied into SeaweedFS
before any QC step; temporary provider URLs are never exposed to the browser.

KWANNI owns its provider runtime configuration; KSys and KDeploy are not
dependencies for this contract. Create `dev/kwanni-video-providers` from
protected files with `FIDELITY_VLM_API_KEY` and at least one of `VEO_API_KEY`
or `SEEDANCE_API_KEY`, then run `scripts/video-provider-runtime.sh wire-api`
with the non-secret model URL/model/revision variables from `.env.example`.
The script validates Secret references without printing their values. ShotVL
continues to use `scripts/shotvl-runtime.sh` and remains private/load-on-demand.

## Branch flow

`feat/*` → `develop` → `main`

## License

Copyright (c) 2026. All rights reserved. Public visibility does not grant an open-source license or determine ownership; see [NOTICE](NOTICE).
