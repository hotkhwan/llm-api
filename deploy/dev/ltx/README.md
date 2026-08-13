# LTX-2.3 challenger (Development)

LTX-2.3 is a project-owned, private challenger which produces synchronized
video and audio from the original product image. It is scaled to zero, has no
public route, and must not replace Hunyuan until a blind DGX benchmark wins on
product fidelity, latency, audio usefulness and memory pressure.

The runtime pins `Lightricks/LTX-2.3` revision `7caa482d...`, exact distilled
and spatial-upscaler files, Gemma 3 revision `68f7ee4f...`, and source revision
`4f890573...`. Both model repositories require accepted Hugging Face terms.
The LTX community license permits qualifying commercial use but has revenue
and downstream-distribution obligations; legal acceptance is a release gate.

Use `scripts/ltx-runtime.sh` only in an operator-controlled exclusive GPU
window. Qwen, Hunyuan, MuseTalk, ShotVL and LTX must never be resident together
until a real scheduler and memory benchmark prove it safe.
