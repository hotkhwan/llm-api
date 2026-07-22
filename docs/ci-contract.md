# CI contract

## Current status: not wired

There is currently no Jenkins job, GitHub Actions workflow, or other remote CI attached to this repository. CI onboarding is blocked on the shared kdeploy D3 work tracked by deployment PR #131. Until that dependency is merged, configured, and observed successfully, no branch or pull request in this repository may claim remote CI coverage.

The intended shared-pipeline job names are:

- `dev-affiliate-api` for an exact merge commit on `develop`
- `prod-affiliate-api` for a separately approved exact commit on `main`

These names describe the integration contract only; the jobs do not exist yet. This public repository intentionally has no environment-specific `Jenkinsfile` and does not define credential IDs, registry locations, cluster details, or deployment endpoints.

## Required inputs

- Git repository and exact 40-character commit SHA
- target stage derived by the deployment platform from the protected branch
- immutable image repository supplied by the deployment platform
- `VERSION` and `VCS_REF` Docker build arguments; `VCS_REF` must equal the exact commit SHA

Secrets, registry credentials, cluster credentials, and signing keys are owned and injected by the deployment platform. They must not be accepted as Docker build arguments or stored in this repository.

## Required exact-SHA gates

1. Record the checked-out commit SHA before running any stage and fail if it differs from the requested SHA.
2. Run `gofmt` verification, `go test ./...`, `go test -race ./internal/...`, `go vet ./...`, and `govulncheck ./...` against that checkout.
3. Scan source and dependencies using platform-approved scanners.
4. Build the digest-pinned `Dockerfile` with an isolated builder, passing the approved version and exact SHA.
5. Scan the image, produce an SBOM and provenance, then push an immutable tag containing the exact SHA.
6. Deploy by image digest and verify `/healthz`, `/readyz`, and `/version`; the reported commit must equal the checked-out SHA.
7. Permit promotion only from evidence belonging to that exact SHA. A green result for another commit is not reusable.

## Required evidence

For each run, retain the job name, build number and URL, protected branch, full checked-out SHA, validation results, image digest, SBOM/provenance references, deployment target, and probe output showing the exact version and commit. A status report must say `CI not wired` until both the shared dependency and named job are operational; local checks are never described as remote CI.
