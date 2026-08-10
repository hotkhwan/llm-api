#!/usr/bin/env bash
set -euo pipefail

namespace=dev
deployment=kwanni-musetalk
model_dir=/opt/local/models/musetalk-1.5
runtime_dir=/opt/local/runtimes/musetalk-1.5
model_revision=3ef28bc5cff08c90ad8178a25f1b570cd800170f
source_revision=0a89dec45a0192b824e3cf4daf96c239440c5ed8
vae_revision=31f26fdeee1355a5c34592e401dd41e45d25a493
whisper_revision=169d4a4341b33bc18d8881c4b69c2e104e1cc0af
dwpose_revision=1a7144101628d69ee7a3768d1ee3a094070dc388
syncnet_revision=405eda8eab9f65c1a6e0c292a5dee5a08089e2ae
prefetch_image='nvcr.io/nvidia/vllm:26.05.post1-py3@sha256:94e21552f644e0c1627464ba89d2f7a4ce7442e196f72afa0bb5d7fba23cbb03'
runtime_image='nvcr.io/nvidia/pytorch@sha256:dcae8df08ef61b019b8eb109113428cba4ef0e37484c6e722406150dd5ada759'

command=${1:-status}
case "$command" in
  prefetch)
    install -d -m 0750 "$model_dir" "$model_dir/face-parse-bisent"
    podman run --rm --network host -v "$model_dir:/models" "$prefetch_image" \
      python3 -c "from huggingface_hub import snapshot_download; snapshot_download(repo_id='TMElyralab/MuseTalk', revision='$model_revision', local_dir='/models'); snapshot_download(repo_id='stabilityai/sd-vae-ft-mse', revision='$vae_revision', local_dir='/models/sd-vae', allow_patterns=['config.json','diffusion_pytorch_model.bin']); snapshot_download(repo_id='openai/whisper-tiny', revision='$whisper_revision', local_dir='/models/whisper', allow_patterns=['config.json','pytorch_model.bin','preprocessor_config.json']); snapshot_download(repo_id='yzd-v/DWPose', revision='$dwpose_revision', local_dir='/models/dwpose', allow_patterns=['dw-ll_ucoco_384.pth']); snapshot_download(repo_id='ByteDance/LatentSync', revision='$syncnet_revision', local_dir='/models/syncnet', allow_patterns=['latentsync_syncnet.pt'])"
    curl -fL https://drive.google.com/uc?id=154JgKpzCPW82qINcVieuPH3fZ2e0P812 -o "$model_dir/face-parse-bisent/79999_iter.pth"
    curl -fL https://download.pytorch.org/models/resnet18-5c106cde.pth -o "$model_dir/face-parse-bisent/resnet18-5c106cde.pth"
    ;;
  prepare)
    install -d -m 0750 "$runtime_dir"
    if test ! -d "$runtime_dir/MuseTalk/.git"; then
      git clone https://github.com/TMElyralab/MuseTalk.git "$runtime_dir/MuseTalk"
    fi
    git -C "$runtime_dir/MuseTalk" fetch --depth=1 origin "$source_revision"
    git -C "$runtime_dir/MuseTalk" checkout --detach "$source_revision"
    install -m 0555 deploy/dev/musetalk/runtime/server.py "$runtime_dir/server.py"
    install -m 0444 deploy/dev/musetalk/runtime/requirements.txt "$runtime_dir/requirements.txt"
    podman run --rm --network host -v "$runtime_dir:/runtime" "$runtime_image" bash -lc \
      'python3 -m venv --system-site-packages /runtime/venv && /runtime/venv/bin/pip install --no-cache-dir -r /runtime/requirements.txt && /runtime/venv/bin/pip install --no-cache-dir mmengine==0.10.7 mmdet==3.1.0 mmpose==1.1.0'
    podman run --rm -v "$runtime_dir:/runtime:ro" "$runtime_image" \
      /runtime/venv/bin/python -c "import torch, cv2, librosa, diffusers; assert torch.version.cuda; print('MuseTalk dependencies: PASS')"
    ;;
  install)
    kubectl apply -k deploy/dev/musetalk
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=0
    ;;
  wire-runtime)
    key_file=${2:?usage: musetalk-runtime.sh wire-runtime /secure/path/musetalk-api-key}
    key=$(tr -d '\r\n' <"$key_file")
    test -n "$key"
    kubectl -n "$namespace" create secret generic kwanni-musetalk-auth --from-literal=MUSETALK_API_KEY="$key" --dry-run=client -o yaml | kubectl apply -f -
    ;;
  up)
    kubectl -n "$namespace" get secret kwanni-musetalk-auth >/dev/null
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=1
    kubectl -n "$namespace" rollout status deployment "$deployment" --timeout=15m
    ;;
  down) kubectl -n "$namespace" scale deployment "$deployment" --replicas=0 ;;
  status) kubectl -n "$namespace" get deployment,pod,service -l app=kwanni-musetalk ;;
  *) echo "usage: $0 {prefetch|prepare|install|wire-runtime KEY_FILE|up|down|status}" >&2; exit 2 ;;
esac
