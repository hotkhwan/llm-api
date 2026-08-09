#!/usr/bin/env bash
set -euo pipefail

readonly namespace="${SHOTVL_NAMESPACE:-dev}"
readonly deployment="kwanni-shotvl"
readonly manifest_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../deploy/dev/shotvl" && pwd)"
readonly image="nvcr.io/nvidia/vllm:26.05.post1-py3@sha256:94e21552f644e0c1627464ba89d2f7a4ce7442e196f72afa0bb5d7fba23cbb03"
readonly model="Vchitect/ShotVL-7B"
readonly revision="fd8c356e8fd8d116417dddb6acc49fbf45f9abbc"
readonly cache_root="/opt/local/models/shotvl"

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "required command missing: $1" >&2; exit 1; }
}

verify_auth_secret() {
  require kubectl
  require base64
  local encoded decoded canonical
  encoded="$(kubectl -n "$namespace" get secret kwanni-shotvl-auth -o jsonpath='{.data.SHOTVL_API_KEY}')"
  decoded="$(printf '%s' "$encoded" | base64 -d)"
  canonical="$(printf '%s' "$decoded" | base64 | tr -d '\n')"
  case "$decoded" in
    ''|*[!A-Za-z0-9._~-]*)
      echo "SHOTVL_API_KEY must be one non-empty HTTP-header-safe value without whitespace" >&2
      exit 1
      ;;
  esac
  if [[ "$canonical" != "$encoded" ]]; then
    echo "SHOTVL_API_KEY contains trailing whitespace or non-canonical bytes" >&2
    exit 1
  fi
}

install_runtime() {
  require kubectl
  kubectl -n "$namespace" get secret kwanni-shotvl-auth >/dev/null 2>&1 || {
    echo "missing Secret dev/kwanni-shotvl-auth; create key SHOTVL_API_KEY from a protected file" >&2
    exit 1
  }
  verify_auth_secret
  kubectl apply -k "$manifest_dir"
  kubectl -n "$namespace" scale deployment "$deployment" --replicas=0
}

verify_api_wiring() {
  require kubectl
  local env_json
  env_json="$(kubectl -n "$namespace" get deployment dev-llm-api -o jsonpath='{.spec.template.spec.containers[0].env}')"
  for name in SHOTVL_URL SHOTVL_MODEL SHOTVL_MODEL_REVISION SHOTVL_THRESHOLD SHOTVL_API_KEY; do
    grep -q "\"name\":\"${name}\"" <<<"$env_json" || {
      echo "dev-llm-api is missing $name wiring" >&2
      exit 1
    }
  done
  grep -q '"secretKeyRef":{"key":"SHOTVL_API_KEY","name":"kwanni-shotvl-auth"}' <<<"$env_json" || {
    echo "SHOTVL_API_KEY is not a Secret reference" >&2
    exit 1
  }
  echo "dev-llm-api ShotVL environment wiring verified (values not printed)"
}

wire_api() {
  require kubectl
  kubectl -n "$namespace" get deployment dev-llm-api >/dev/null
  kubectl -n "$namespace" get secret kwanni-shotvl-auth >/dev/null
  verify_auth_secret
  kubectl -n "$namespace" set env deployment/dev-llm-api \
    SHOTVL_URL=http://kwanni-shotvl.dev.svc.cluster.local:8000/v1 \
    SHOTVL_MODEL=shotvl-7b \
    SHOTVL_MODEL_REVISION="$revision" \
    SHOTVL_THRESHOLD=0.75
  kubectl -n "$namespace" set env deployment/dev-llm-api \
    --from=secret/kwanni-shotvl-auth
  kubectl -n "$namespace" rollout status deployment/dev-llm-api --timeout=5m
  verify_api_wiring
}

unwire_api() {
  require kubectl
  kubectl -n "$namespace" set env deployment/dev-llm-api \
    SHOTVL_URL- SHOTVL_MODEL- SHOTVL_MODEL_REVISION- SHOTVL_THRESHOLD- SHOTVL_API_KEY-
  kubectl -n "$namespace" rollout status deployment/dev-llm-api --timeout=5m
}

prefetch() {
  require docker
  install -d -m 0750 "$cache_root"
  docker run --rm \
    --network bridge \
    --mount "type=bind,src=${cache_root},dst=/models" \
    -e HF_HOME=/models/huggingface \
    -e HF_HUB_DISABLE_TELEMETRY=1 \
    "$image" \
    python3 -c 'from huggingface_hub import snapshot_download; snapshot_download("'"$model"'", revision="'"$revision"'", local_files_only=False)'
}

up() {
  require kubectl
  test -d "$cache_root/huggingface/hub/models--Vchitect--ShotVL-7B/snapshots/$revision" || {
    echo "immutable ShotVL snapshot is absent; run: $0 prefetch" >&2
    exit 1
  }
  verify_api_wiring
  install_runtime
  kubectl -n "$namespace" scale deployment "$deployment" --replicas=1
  kubectl -n "$namespace" rollout status deployment "$deployment" --timeout=20m
}

down() {
  require kubectl
  kubectl -n "$namespace" scale deployment "$deployment" --replicas=0
  kubectl -n "$namespace" wait --for=delete pod -l app.kubernetes.io/name=kwanni-shotvl --timeout=5m || true
}

status() {
  require kubectl
  kubectl -n "$namespace" get deployment,pod,service -l app.kubernetes.io/name=kwanni-shotvl
}

case "${1:-}" in
  install) install_runtime ;;
  prefetch) prefetch ;;
  wire-api) wire_api ;;
  verify-api) verify_api_wiring ;;
  unwire-api) unwire_api ;;
  up) up ;;
  down) down ;;
  status) status ;;
  *) echo "usage: $0 {install|prefetch|wire-api|verify-api|unwire-api|up|down|status}" >&2; exit 2 ;;
esac
