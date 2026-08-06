# KWANNI Mission Zero backend — Phase 1

## Outcome

This phase proves one coherent API behavior:

`manual product → missionAccepted → captureStarted → assetsUploaded → draftReady → exported → posted`

It intentionally does not implement trend aggregation, automatic publishing,
credits, a marketplace, or income claims.

## Runtime boundaries

- `Repository` is an optimistic compare-and-swap contract. A MongoDB adapter
  must update by `{_id, version}` and increment `version`, returning a conflict
  when another worker has already advanced the mission.
- `ObjectStore` models the S3 API served by SeaweedFS. Production code must use
  a dedicated bucket/prefix and credentials delivered through the platform
  secret provider. The domain has no MinIO-named type or dependency.
- `CaptionGenerator` accepts product facts and produces structured caption
  output. `OpenAICompatibleCaptioner` can call the private local endpoint;
  `FallbackCaptioner` ensures model downtime cannot block the first post.
- `AuditSink` records state changes and actual generation units/cost. The
  production adapter should use an atomic outbox with mission state changes.

The process currently wires in-memory adapters so the slice is testable before
cluster secrets and databases are available. It is not durable and must not be
used as production persistence.

## Media behavior

Each mission requires exactly three image or video assets. The API records the
SeaweedFS object key, MIME type, size, and SHA-256 digest. Draft generation
creates a three-clip vertical-video edit plan and a truthful caption. Export
currently reserves the immutable target
`missions/{missionId}/exports/first-post.mp4`; rendering the bytes is delegated
to the upcoming FFmpeg worker.

Asset bytes are sniffed and must match the declared media type. Retrying an
identical shot, draft, export, or mark-posted operation returns the existing
result without changing its version or adding duplicate ledger entries;
conflicting retries fail closed.
The domain also verifies that the storage adapter returns the requested key,
media type, byte count, and SHA-256 digest before recording an asset.

## Next implementation backlog

1. MongoDB repository plus indexes and transactional outbox.
2. SeaweedFS S3 adapter with bounded streaming upload, bucket policy, and
   presigned download.
3. FFmpeg worker with idempotency key, resume/retry, and output verification.
4. Authentication adapter that derives `userId` from Klynx identity rather
   than accepting it from the request body.
5. Persisted audit/cost ledger and provider retry-reserve accounting.
6. Click/sale result recording, next-mission generation, and activation events.
7. Privacy notice/consent evidence and media retention/deletion workflow.

## Acceptance checks

`go test ./...` includes a service-level flow and an HTTP flow through all Phase
1 states. It also covers invalid transitions, duplicate shots, media-type
validation, local-LLM structured output, and deterministic fallback.
