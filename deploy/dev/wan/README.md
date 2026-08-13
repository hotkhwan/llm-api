# Wan2.2 local preview (Development)

This KWANNI-owned runtime renders one five-second portrait preview from the
original product image. It is not part of KSys or KDeploy, has no public route,
and is scaled to zero by default. The durable API job stores its ETA and output
in MongoDB/SeaweedFS, so a user can close the browser and return later.

Immutable upstream inputs:

- Model `Wan-AI/Wan2.2-TI2V-5B`, revision
  `921dbaf3f1674a56f47e83fb80a34bac8a8f203e`
- Runtime source `Wan-Video/Wan2.2`, revision
  `42bf4cfaa384bc21833865abc2f9e6c0e67233dc`
- NVIDIA PyTorch 26.06 ARM64 base manifest
  `sha256:dcae8df08ef61b019b8eb109113428cba4ef0e37484c6e722406150dd5ada759`
- Output contract: one 5-second, 704x1280, 121-frame MP4 at 24 FPS

The upstream model card reports less than nine minutes for a five-second 720p
clip on an RTX 4090 before optimization. A 2026-08-10 point-DGX baseline using
the pinned official pipeline, 121 frames and 50 steps took 24m38s end-to-end:
18m59s diffusion at 22.78s/step plus model load, VAE decode and MP4 encode. The
result was H.264, 24 FPS, 121 frames and 5.041667 seconds. Keep the UI estimate
at 20-30 minutes until a faster reviewed preset has equivalent quality evidence.

This was an operator pipeline benchmark, not end-to-end API acceptance. Qwen
co-residency produced CUDA OOM and llama.cpp automatic idle unload did not
activate reliably with the current MTP service. Keep Wan scaled to zero and
unwired on the unauthenticated Alpha site; use an operator-controlled exclusive
GPU window until a tested scheduler, login and quota enforcement land.

## Operator flow

The Deployment uses NVIDIA's pinned ARM64 PyTorch base directly. `prepare`
installs the exact source and pinned pure-Python dependencies into the private
host runtime directory; it does not build a container image.
Dependencies are installed with `--no-deps` so pip cannot shadow NVIDIA's
CUDA-enabled Torch with a public wheel; the dependency smoke must pass before
the Deployment is scaled up. Wan itself initializes CUDA at import time, so the
full import/generation smoke runs inside the K3s NVIDIA RuntimeClass.

`prepare` also installs the reviewed `runtime/wan_init.py` compatibility shim
as a Python namespace overlay, leaving the pinned upstream checkout clean.
The pinned upstream package eagerly imports speech/animate modules and their
optional dependencies even for TI2V; the shim exposes only `WanTI2V` and does
not alter model weights or inference code.

```bash
sudo scripts/wan-runtime.sh prefetch
sudo scripts/wan-runtime.sh prepare
scripts/wan-runtime.sh install
scripts/wan-runtime.sh wire-api /secure/path/wan-api-key
scripts/wan-runtime.sh up
scripts/wan-runtime.sh status
scripts/wan-runtime.sh down
```

`prefetch` downloads the exact public model revision under
`/opt/local/models/wan2.2-ti2v-5b`. `wire-api` creates two namespace Secrets
from a root-owned key file and patches only `WAN_URL`, `WAN_TIMEOUT`, and the
secret-backed `WAN_API_KEY` onto `dev-llm-api`. No credential is printed or
stored in Git.

Keep one global job only. Qwen plans first, the operator releases its GPU
allocation, Wan renders, Wan scales down, then Qwen is restored before ShotVL
runs in its own window. Never schedule those three GPU workloads concurrently.

`LTX-Video 2B 0.9.8 distilled` is the reviewed fast-preview challenger because
it is I2V and materially smaller. Do not make it primary without a blind product
fidelity benchmark and legal acceptance of its custom Open Weights License;
Wan remains the Apache-2.0 quality baseline.

Wan local preview is not product-fidelity proof. Human approval remains
required because small logos, labels, colors, and geometry may drift. Paid
Veo/Seedance promotion is a separate, authenticated operator action and is not
automatic.
