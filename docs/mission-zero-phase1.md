# KWANNI Mission Zero backend

## User outcome

`manual product → product reference → mission → replaceable 3-shot capture → draft → verified MP4 export → mark posted → record outcome → next action`

The API does not aggregate trends, publish automatically, sell credits, promise
income, or run an affiliate marketplace.

## Production brain

One Qwen3.6 runtime performs the creative, story, brand-guard, production-plan
and prompt-compiler roles in a single structured request. Its output is the
versioned `kwanni.production/v1` Canonical Production Spec: story beats, three
shots, provider-neutral camera/lighting data, provider compilers and product,
character, wardrobe, makeup, location, lighting and camera continuity bibles.

The deterministic planner is a fail-safe when Qwen is absent, slow or emits an
invalid/unsafe schema. It still produces a complete CPS from verified facts.
ShotVL is an optional `VisualQCQueue`, loaded on demand after keyframe
extraction. It is not resident and never blocks First Mission or First Post.

## Durable runtime

- MongoDB persists missions with `{id, version}` compare-and-swap, audit/cost
  events, export job payloads and a unique job idempotency key.
- SeaweedFS is accessed through its standard S3 endpoint. Assets and output
  record exact byte count and SHA-256; downloads use short-lived signed URLs.
- The synchronous Mission Zero FFmpeg processor claims an idempotent job,
  downloads exactly three bounded assets, normalizes each segment to vertical
  1080x1920 H.264, concatenates them, verifies codec/dimensions with ffprobe,
  and only then uploads and reports `exported`. Failed jobs are retryable.
- Production fails configuration validation unless MongoDB and SeaweedFS
  endpoint/credentials are present. Development/test may use in-memory
  adapters explicitly for tests; they are not production evidence.

Runtime secrets are `MONGO_URI`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` and optional
`LOCAL_LLM_API_KEY`; Kubernetes Secret injects them. The local AI bridge uses
`LOCAL_LLM_URL=http://local-ai-bridge.local-ai.svc.cluster.local:18082/v1` and
the selected catalog alias in `LOCAL_LLM_MODEL`. Secrets must not enter source,
logs, command arguments, examples, or mission records.

## Security and privacy

The HTTP boundary ignores caller-supplied user IDs and requires the trusted
gateway identity header `X-Authenticated-User-ID`. Every mission route verifies
ownership without revealing another user's mission. Creation requires explicit
privacy-notice consent and stores its version/time. Product and capture bytes
are MIME-sniffed and size-bounded. A different PUT replaces a mistaken image or
shot only before drafting; an identical retry is idempotent.

## Retry and resume

Mission state is optimistic-CAS protected. Uploads, references, draft, export,
mark-posted and outcome retries return the existing result when identical.
Draft generation can resume from `draftGenerating`. Export jobs are keyed by
mission/version, persist failure evidence, and retry the same work rather than
creating duplicate artifacts.

## Deferred beyond Mission Zero

- Autonomous publishing, marketplace, wallet and cross-platform trend crawl.
- A resident VLM. ShotVL remains advisory/load-on-demand until its benchmark
  and operational envelope pass.
- The 35B challenger. Qwen3.6-27B-Q8_0-MTP-16K remains primary until blind
  production-spec evaluation shows a material quality gain.
