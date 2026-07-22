# CI contract

This repository intentionally has no environment-specific `Jenkinsfile`. The deployment platform should onboard it through the kdeploy D3 shared pipeline without copying credential IDs or cluster details into this public repository.

## Required inputs

- Git repository and exact commit SHA
- target stage derived by the deployment platform from the protected branch
- immutable image repository supplied by the deployment platform
- `VERSION` and `VCS_REF` Docker build arguments; `VCS_REF` must equal the exact commit SHA

Secrets, registry credentials, cluster credentials, and signing keys are owned and injected by the deployment platform. They must not be accepted as Docker build arguments or stored in this repository.

## Required stages

1. Run `gofmt` verification, `go test ./...`, and `go vet ./...`.
2. Scan source and dependencies using the platform-approved scanners.
3. Build the digest-pinned `Dockerfile` with an isolated builder.
4. Scan the resulting image, produce an SBOM, and attach provenance.
5. Push an immutable image tagged with the exact commit SHA.
6. Deploy by image digest, then verify `/healthz`, `/readyz`, and `/version`.

CI must fail when `/version` does not report the supplied `VERSION` and exact `VCS_REF`. Branch-to-environment mapping and promotion policy remain deployment-platform configuration, not application code.
