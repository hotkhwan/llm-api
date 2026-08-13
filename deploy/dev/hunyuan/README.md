# HunyuanVideo-1.5 product preview (Development)

This project-owned, private runtime is KWANNI's product-preview baseline. It
uses the official 480p I2V step-distilled checkpoint (12 steps), always receives
the original product image, has no public route, and is scaled to zero by
default. It is not part of KSys or KDeploy.

Pinned inputs:

- `tencent/HunyuanVideo-1.5` model revision `9b49404b...`, transformer
  `480p_i2v_step_distilled`
- `Tencent-Hunyuan/HunyuanVideo-1.5` source revision `60783e70...`
- Qwen2.5-VL-7B, ByT5, Glyph-SDXL-v2 and FLUX.1-Redux vision encoder revisions
  are pinned in `scripts/hunyuan-runtime.sh`
- Glyph-SDXL-v2 is fetched with pinned ModelScope `1.39.1` and the runtime
  refuses to continue unless the real `checkpoints/byt5_model.pt` is present;
  a Git checkout without LFS is not accepted
- NVIDIA ARM64 PyTorch image digest `sha256:dcae8df0...`

The FLUX.1-Redux vision encoder is gated. An operator must first accept its
Hugging Face terms and pass a root-owned token file to `prefetch`; no token is
stored in Git or Kubernetes. This gate means the runtime must not be advertised
as available until the full prefetch and one DGX benchmark pass.

The Tencent Hunyuan Community License excludes the EU, United Kingdom and
South Korea from its licensed territory and requires end-user provider and
non-affiliation disclosures. KWANNI's initial Thailand/China test audience is
inside the stated territory, but an unauthenticated global endpoint is not.
External enablement therefore also requires region enforcement and reviewed
Terms/Acceptable Use notices; the operator-only flag is not a legal substitute.

```bash
sudo scripts/hunyuan-runtime.sh prefetch /secure/path/hf-token
sudo scripts/hunyuan-runtime.sh prepare
scripts/hunyuan-runtime.sh install
scripts/hunyuan-runtime.sh wire-api /secure/path/hunyuan-api-key
scripts/hunyuan-runtime.sh up
scripts/hunyuan-runtime.sh status
scripts/hunyuan-runtime.sh down
```

Use an exclusive GPU window: stop Qwen before scaling Hunyuan up, scale
Hunyuan down after the durable preview job finishes, then restore Qwen.
Hunyuan does not generate audio; the presenter/audio stage is separate.
Product fidelity is advisory until visual/OCR and human review pass.

NVIDIA PyTorch 26.06 currently includes a newer torchao in which `NF4Tensor`
moved. Pinned diffusers 0.35.0 handles that as a warning but initializes its
logger too late, raising `NameError` during import. `prepare` applies one exact,
fail-closed source-order compatibility patch after installation; it does not
change inference or model weights and refuses unknown diffusers source.
