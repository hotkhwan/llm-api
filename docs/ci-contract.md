# CI contract

## Current status: not wired

There is currently no Jenkins job, GitHub Actions workflow, or other remote CI attached to this repository. The restricted/private [klynx-cluster-deploy PR #131](https://github.com/pointitconsulting/klynx-cluster-deploy/pull/131) is a D1 security prerequisite only; it does not implement D3. The later D3 onboarding work remains unimplemented, and no D3 job exists for this repository. Until D3 is implemented, configured, and observed successfully, no branch or pull request in this repository may claim remote CI coverage.

The intended shared-pipeline job names are:

- `dev-llm-api` for an exact merge commit on `develop`
- `prod-llm-api` for a separately approved exact commit on `main`

These names describe the integration contract only; the jobs do not exist yet. This public repository intentionally has no environment-specific `Jenkinsfile` and does not define credential IDs, registry locations, cluster details, or deployment endpoints.

## Required inputs

- Git repository and exact 40-character commit SHA
- target stage derived by the deployment platform from the protected branch
- immutable image repository supplied by the deployment platform
- authoritative SemVer read from the repository-root `VERSION` file
- `VERSION` and `VCS_REF` Docker build arguments; `VERSION` must equal the file exactly and `VCS_REF` must equal the exact full commit SHA

Secrets, registry credentials, cluster credentials, and signing keys are owned and injected by the deployment platform. They must not be accepted as Docker build arguments or stored in this repository.

## Required exact-SHA gates

1. Record the checked-out commit SHA before running any stage and fail if it differs from the requested SHA.
2. Read and validate the authoritative version from the checkout with `version="$(./scripts/verify-version.sh)"`. Do not derive or override it from a Git tag, branch, package, or CI setting.
3. Validate the version/SHA pair with `./scripts/verify-version.sh "$version" "$source_sha"`, where `source_sha` is the recorded 40-character lowercase checkout SHA. Any malformed value or mismatch must fail the run.
4. Run `./scripts/verify-version_test.sh`, `gofmt` verification, `go test ./...`, `go test -race ./internal/...`, `go vet ./...`, and `govulncheck ./...` against that checkout.
5. Scan source and dependencies using platform-approved scanners.
6. Build the digest-pinned `Dockerfile` with an isolated builder, passing `--build-arg VERSION="$version"` and `--build-arg VCS_REF="$source_sha"`. The Dockerfile independently verifies the values against `VERSION` before compiling.
7. Require the runtime `GET /version` response and OCI labels `org.opencontainers.image.version` / `org.opencontainers.image.revision` to equal the validated version and full source SHA. Fail instead of publishing when any value differs.
8. Scan the image, produce an SBOM and provenance, then push an immutable tag containing the exact full SHA (a short SHA may be appended only as a display alias, never as the recorded revision).
9. Deploy by image digest and verify `/healthz`, `/readyz`, and `/version`; the reported version and commit must equal the validated values.
10. Permit promotion only from evidence belonging to that exact version/SHA pair. A green result for another commit is not reusable.

## Required evidence

For each run, retain the job name, build number and URL, protected branch, exact `VERSION` content, full checked-out SHA, validation results, OCI version/revision labels, image digest, SBOM/provenance references, deployment target, and probe output showing the exact version and commit. A status report must say `CI not wired` until both the shared dependency and named job are operational; local checks are never described as remote CI.
