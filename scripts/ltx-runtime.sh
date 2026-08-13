#!/usr/bin/env bash
set -euo pipefail

namespace=dev
deployment=kwanni-ltx
model_dir=/opt/local/models/ltx-2.3
runtime_dir=/opt/local/runtimes/ltx-2.3
model_revision=7caa482d5cd10a2eae6b34cb48f093ebc45a263e
gemma_revision=68f7ee4fbd59087436ada77ed2d62f373fdd4482
source_revision=4f8905737aac86a554637cac86c178877a39c744
prefetch_image='nvcr.io/nvidia/vllm:26.05.post1-py3@sha256:94e21552f644e0c1627464ba89d2f7a4ce7442e196f72afa0bb5d7fba23cbb03'
runtime_image='nvcr.io/nvidia/pytorch@sha256:dcae8df08ef61b019b8eb109113428cba4ef0e37484c6e722406150dd5ada759'

command=${1:-status}
case "$command" in
  prefetch)
    token_file=${2:?usage: ltx-runtime.sh prefetch /secure/path/huggingface-token}
    token=$(tr -d '\r\n' <"$token_file")
    test -n "$token"
    install -d -m 0750 "$model_dir"
    podman run --rm --network host -v "$model_dir:/models" -e HF_TOKEN="$token" "$prefetch_image" \
      python3 -c "from huggingface_hub import snapshot_download; snapshot_download(repo_id='Lightricks/LTX-2.3', revision='$model_revision', local_dir='/models', allow_patterns=['ltx-2.3-22b-distilled-1.1.safetensors','ltx-2.3-spatial-upscaler-x2-1.1.safetensors']); snapshot_download(repo_id='google/gemma-3-12b-it-qat-q4_0-unquantized', revision='$gemma_revision', local_dir='/models/gemma-3-12b')"
    ;;
  prepare)
    install -d -m 0750 "$runtime_dir"
    if test ! -d "$runtime_dir/LTX-2/.git"; then
      git clone https://github.com/Lightricks/LTX-2.git "$runtime_dir/LTX-2"
    fi
    git -C "$runtime_dir/LTX-2" fetch --depth=1 origin "$source_revision"
    git -C "$runtime_dir/LTX-2" checkout --detach "$source_revision"
    install -m 0555 deploy/dev/ltx/runtime/server.py "$runtime_dir/server.py"
    podman run --rm --network host -v "$runtime_dir:/runtime" "$runtime_image" bash -lc \
      'python3 -m venv --system-site-packages /runtime/venv && /runtime/venv/bin/pip install --no-cache-dir -e /runtime/LTX-2/packages/ltx-core -e /runtime/LTX-2/packages/ltx-pipelines'
    podman run --rm -v "$runtime_dir:/runtime:ro" "$runtime_image" \
      /runtime/venv/bin/python -c "import torch, ltx_core, ltx_pipelines; assert torch.version.cuda; print('LTX dependencies: PASS')"
    ;;
  install)
    kubectl apply -k deploy/dev/ltx
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=0
    ;;
  wire-api)
    key_file=${2:?usage: ltx-runtime.sh wire-api /secure/path/ltx-api-key}
    key=$(tr -d '\r\n' <"$key_file")
    test -n "$key"
    kubectl -n "$namespace" create secret generic kwanni-ltx-auth --from-literal=LTX_API_KEY="$key" --dry-run=client -o yaml | kubectl apply -f -
    kubectl -n "$namespace" create secret generic dev-llm-api-ltx --from-literal=LTX_API_KEY="$key" --dry-run=client -o yaml | kubectl apply -f -
    kubectl -n "$namespace" set env deployment/dev-llm-api LTX_URL=http://kwanni-ltx.dev.svc.cluster.local:8092 LTX_TIMEOUT=20m
    kubectl -n "$namespace" set env deployment/dev-llm-api --from=secret/dev-llm-api-ltx
    kubectl -n "$namespace" rollout status deployment/dev-llm-api --timeout=180s
    ;;
  unwire-api)
    kubectl -n "$namespace" set env deployment/dev-llm-api LTX_URL- LTX_TIMEOUT- LTX_API_KEY-
    kubectl -n "$namespace" delete secret dev-llm-api-ltx --ignore-not-found
    ;;
  up)
    kubectl -n "$namespace" get secret kwanni-ltx-auth >/dev/null
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=1
    kubectl -n "$namespace" rollout status deployment "$deployment" --timeout=30m
    ;;
  down) kubectl -n "$namespace" scale deployment "$deployment" --replicas=0 ;;
  status) kubectl -n "$namespace" get deployment,pod,service -l app=kwanni-ltx ;;
  *) echo "usage: $0 {prefetch TOKEN_FILE|prepare|install|wire-api KEY_FILE|unwire-api|up|down|status}" >&2; exit 2 ;;
esac
