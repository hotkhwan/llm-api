#!/usr/bin/env bash
set -euo pipefail

namespace=dev
deployment=kwanni-hunyuan
model_dir=/opt/local/models/hunyuan-video-1.5
runtime_dir=/opt/local/runtimes/hunyuan-video-1.5
model_revision=9b49404b3f5df2a8f0b31df27a0c7ab872e7b038
qwen_revision=cc594898137f460bfe9f0759e9844b3ce807cfb5
byt5_revision=68377bdc18a2ffec8a0533fef03b1c513a4dd49d
flux_revision=c95859fbf7703ca4d6824b4da4407d7cd0434f81
glyph_revision=b51a8604c03c9a4e71a389f4c7deeb526320f2e6
source_revision=60783e704160023913bee78f0b47036d393d4dfa
prefetch_image='nvcr.io/nvidia/vllm:26.05.post1-py3@sha256:94e21552f644e0c1627464ba89d2f7a4ce7442e196f72afa0bb5d7fba23cbb03'
runtime_image='nvcr.io/nvidia/pytorch@sha256:dcae8df08ef61b019b8eb109113428cba4ef0e37484c6e722406150dd5ada759'

command=${1:-status}
case "$command" in
  prefetch)
    token_file=${2:?usage: hunyuan-runtime.sh prefetch /secure/path/huggingface-token}
    test -f "$token_file"
    token=$(tr -d '\r\n' <"$token_file")
    test -n "$token"
    install -d -m 0750 "$model_dir"
    podman run --rm --network host -v "$model_dir:/models" -e HF_TOKEN="$token" "$prefetch_image" \
      python3 -c "from huggingface_hub import snapshot_download; snapshot_download(repo_id='tencent/HunyuanVideo-1.5', revision='$model_revision', local_dir='/models', allow_patterns=['config.json','scheduler/*','vae/*','transformer/480p_i2v_step_distilled/*']); snapshot_download(repo_id='Qwen/Qwen2.5-VL-7B-Instruct', revision='$qwen_revision', local_dir='/models/text_encoder/llm'); snapshot_download(repo_id='google/byt5-small', revision='$byt5_revision', local_dir='/models/text_encoder/byt5-small'); snapshot_download(repo_id='black-forest-labs/FLUX.1-Redux-dev', revision='$flux_revision', local_dir='/models/vision_encoder/siglip')"
    podman run --rm --network host -v "$model_dir:/models" "$prefetch_image" \
      sh -ceu "python3 -m pip install --no-cache-dir modelscope==1.39.1 >/dev/null; python3 -c \"from modelscope import snapshot_download; snapshot_download('AI-ModelScope/Glyph-SDXL-v2', revision='$glyph_revision', local_dir='/models/text_encoder/Glyph-SDXL-v2')\""
    test -s "$model_dir/text_encoder/Glyph-SDXL-v2/checkpoints/byt5_model.pt"
    ;;
  prepare)
    install -d -m 0750 "$runtime_dir" "$runtime_dir/python"
    if test ! -d "$runtime_dir/HunyuanVideo-1.5/.git"; then
      git clone https://github.com/Tencent-Hunyuan/HunyuanVideo-1.5.git "$runtime_dir/HunyuanVideo-1.5"
    fi
    git -C "$runtime_dir/HunyuanVideo-1.5" fetch --depth=1 origin "$source_revision"
    git -C "$runtime_dir/HunyuanVideo-1.5" checkout --detach "$source_revision"
    install -m 0555 deploy/dev/hunyuan/runtime/server.py "$runtime_dir/server.py"
    install -m 0555 deploy/dev/hunyuan/runtime/patch_diffusers.py "$runtime_dir/patch_diffusers.py"
    install -m 0444 deploy/dev/hunyuan/runtime/requirements.txt "$runtime_dir/requirements.txt"
    podman run --rm --network host -v "$runtime_dir:/runtime" "$runtime_image" \
      python3 -m pip install --no-cache-dir --no-deps --target /runtime/python -r /runtime/requirements.txt
    podman run --rm -v "$runtime_dir:/runtime" "$runtime_image" \
      python3 /runtime/patch_diffusers.py
    podman run --rm -v "$runtime_dir:/runtime:ro" -v "$model_dir:/models:ro" \
      -e PYTHONPATH=/runtime/python:/runtime/HunyuanVideo-1.5 "$runtime_image" \
      python3 -c "import torch, transformers, diffusers; from hyvideo.pipelines.hunyuan_video_pipeline import HunyuanVideo_1_5_Pipeline; assert torch.version.cuda; assert HunyuanVideo_1_5_Pipeline.get_transformer_version('480p','i2v',False,True,False) == '480p_i2v_step_distilled'; print('Hunyuan dependencies: PASS')"
    ;;
  install)
    kubectl apply -k deploy/dev/hunyuan
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=0
    ;;
  wire-api)
    key_file=${2:?usage: hunyuan-runtime.sh wire-api /secure/path/hunyuan-api-key}
    test -f "$key_file"
    key=$(tr -d '\r\n' <"$key_file")
    test -n "$key"
    kubectl -n "$namespace" create secret generic kwanni-hunyuan-auth --from-literal=HUNYUAN_API_KEY="$key" --dry-run=client -o yaml | kubectl apply -f -
    kubectl -n "$namespace" create secret generic dev-llm-api-hunyuan --from-literal=HUNYUAN_API_KEY="$key" --dry-run=client -o yaml | kubectl apply -f -
    kubectl -n "$namespace" set env deployment/dev-llm-api HUNYUAN_URL=http://kwanni-hunyuan.dev.svc.cluster.local:8091 HUNYUAN_TIMEOUT=15m
    kubectl -n "$namespace" set env deployment/dev-llm-api --from=secret/dev-llm-api-hunyuan
    kubectl -n "$namespace" rollout status deployment/dev-llm-api --timeout=180s
    ;;
  unwire-api)
    kubectl -n "$namespace" set env deployment/dev-llm-api HUNYUAN_URL- HUNYUAN_TIMEOUT- HUNYUAN_API_KEY-
    kubectl -n "$namespace" delete secret dev-llm-api-hunyuan --ignore-not-found
    ;;
  up)
    kubectl -n "$namespace" get secret kwanni-hunyuan-auth >/dev/null
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=1
    kubectl -n "$namespace" rollout status deployment "$deployment" --timeout=20m
    ;;
  down)
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=0
    ;;
  status)
    kubectl -n "$namespace" get deployment,pod,service -l app=kwanni-hunyuan
    ;;
  *)
    echo "usage: $0 {prefetch TOKEN_FILE|prepare|install|wire-api KEY_FILE|unwire-api|up|down|status}" >&2
    exit 2
    ;;
esac
