#!/usr/bin/env bash
set -euo pipefail

namespace=dev
deployment=kwanni-wan
model_dir=/opt/local/models/wan2.2-ti2v-5b
runtime_dir=/opt/local/runtimes/wan2.2
model_revision=921dbaf3f1674a56f47e83fb80a34bac8a8f203e
source_revision=42bf4cfaa384bc21833865abc2f9e6c0e67233dc
prefetch_image='nvcr.io/nvidia/vllm:26.05.post1-py3@sha256:94e21552f644e0c1627464ba89d2f7a4ce7442e196f72afa0bb5d7fba23cbb03'
runtime_image='nvcr.io/nvidia/pytorch@sha256:dcae8df08ef61b019b8eb109113428cba4ef0e37484c6e722406150dd5ada759'

command=${1:-status}
case "$command" in
  prefetch)
    install -d -m 0750 "$model_dir"
    podman run --rm --network host -v "$model_dir:/models" "$prefetch_image" \
      python3 -c "from huggingface_hub import snapshot_download; snapshot_download(repo_id='Wan-AI/Wan2.2-TI2V-5B', revision='$model_revision', local_dir='/models')"
    ;;
  prepare)
    install -d -m 0750 "$runtime_dir" "$runtime_dir/python" "$runtime_dir/overlay" "$runtime_dir/overlay/wan"
    if test ! -d "$runtime_dir/Wan2.2/.git"; then
      git clone https://github.com/Wan-Video/Wan2.2.git "$runtime_dir/Wan2.2"
    fi
    git -C "$runtime_dir/Wan2.2" fetch --depth=1 origin "$source_revision"
    git -C "$runtime_dir/Wan2.2" checkout --detach "$source_revision"
    install -m 0555 deploy/dev/wan/runtime/server.py "$runtime_dir/server.py"
    install -m 0555 deploy/dev/wan/runtime/launcher.py "$runtime_dir/launcher.py"
    install -m 0444 deploy/dev/wan/runtime/requirements.txt "$runtime_dir/requirements.txt"
    install -m 0444 deploy/dev/wan/runtime/wan_init.py "$runtime_dir/overlay/wan/__init__.py"
    podman run --rm --network host -v "$runtime_dir:/runtime" "$runtime_image" \
      python3 -m pip install --no-cache-dir --no-deps --target /runtime/python -r /runtime/requirements.txt
    podman run --rm -v "$runtime_dir:/runtime:ro" \
      -e PYTHONPATH=/runtime/python:/runtime/overlay:/runtime/Wan2.2 "$runtime_image" \
      python3 -c "import importlib.util, torch, transformers, diffusers; assert torch.version.cuda and importlib.util.find_spec('flash_attn'); print('Wan dependencies: PASS')"
    ;;
  install)
    kubectl apply -k deploy/dev/wan
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=0
    ;;
  wire-api)
    key_file=${2:?usage: wan-runtime.sh wire-api /secure/path/wan-api-key}
    test -f "$key_file"
    key=$(tr -d '\r\n' <"$key_file")
    test -n "$key"
    kubectl -n "$namespace" create secret generic kwanni-wan-auth --from-literal=WAN_API_KEY="$key" --dry-run=client -o yaml | kubectl apply -f -
    kubectl -n "$namespace" create secret generic dev-llm-api-wan --from-literal=WAN_API_KEY="$key" --dry-run=client -o yaml | kubectl apply -f -
    kubectl -n "$namespace" set env deployment/dev-llm-api WAN_URL=http://kwanni-wan.dev.svc.cluster.local:8090 WAN_TIMEOUT=45m
    kubectl -n "$namespace" set env deployment/dev-llm-api --from=secret/dev-llm-api-wan
    kubectl -n "$namespace" rollout status deployment/dev-llm-api --timeout=180s
    ;;
  unwire-api)
    kubectl -n "$namespace" set env deployment/dev-llm-api WAN_URL- WAN_TIMEOUT- WAN_API_KEY-
    kubectl -n "$namespace" delete secret dev-llm-api-wan --ignore-not-found
    ;;
  up)
    kubectl -n "$namespace" get secret kwanni-wan-auth >/dev/null
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=1
    kubectl -n "$namespace" rollout status deployment "$deployment" --timeout=20m
    ;;
  down)
    kubectl -n "$namespace" scale deployment "$deployment" --replicas=0
    ;;
  status)
    kubectl -n "$namespace" get deployment,pod,service -l app=kwanni-wan
    ;;
  *)
    echo "usage: $0 {prefetch|prepare|install|wire-api KEY_FILE|unwire-api|up|down|status}" >&2
    exit 2
    ;;
esac
