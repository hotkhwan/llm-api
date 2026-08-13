# MuseTalk presenter preview (Development)

MuseTalk is deliberately separate from product image-to-video. It lip-syncs a
consented presenter source video to supplied narration audio; it does not make
a presenter or full commercial from a product photo. The private Deployment is
scaled to zero and has no public route.

Pinned model revision: `TMElyralab/MuseTalk@3ef28bc5...`; pinned source:
`TMElyralab/MuseTalk@0a89dec4...`. MuseTalk supports several languages and its
upstream card permits commercial use, but presenter consent, retention/deletion
and licensed narration audio are mandatory before portal/API exposure.

The internal `/present` contract requires two data URLs: `presenterVideo`
(`video/mp4`) and `audio` (`audio/wav` or `audio/mpeg`). It never accepts the
product reference as a face. `scripts/musetalk-runtime.sh` prepares and tests
the runtime; API wiring remains intentionally disabled until the consent/media
contract lands.
