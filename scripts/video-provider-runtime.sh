#!/usr/bin/env bash
set -euo pipefail

readonly namespace="${KWANNI_NAMESPACE:-dev}"
readonly api_deployment="${KWANNI_API_DEPLOYMENT:-dev-llm-api}"
readonly secret_name="${KWANNI_VIDEO_PROVIDER_SECRET:-kwanni-video-providers}"

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "required command missing: $1" >&2; exit 1; }
}

secret_has_key() {
  kubectl -n "$namespace" get secret "$secret_name" \
    -o "jsonpath={.data.$1}" 2>/dev/null | grep -q .
}

validate_contract() {
  require kubectl
  kubectl -n "$namespace" get deployment "$api_deployment" >/dev/null
  kubectl -n "$namespace" get secret "$secret_name" >/dev/null || {
    echo "missing Secret $namespace/$secret_name" >&2
    echo "create it from protected files; never pass credentials as command-line literals" >&2
    exit 1
  }

  secret_has_key FIDELITY_VLM_API_KEY || {
    echo "$secret_name must contain FIDELITY_VLM_API_KEY" >&2
    exit 1
  }
  if ! secret_has_key VEO_API_KEY && ! secret_has_key SEEDANCE_API_KEY; then
    echo "$secret_name must contain VEO_API_KEY and/or SEEDANCE_API_KEY" >&2
    exit 1
  fi

  : "${FIDELITY_VLM_URL:?set FIDELITY_VLM_URL}"
  : "${FIDELITY_VLM_MODEL:?set FIDELITY_VLM_MODEL}"
  : "${FIDELITY_VLM_MODEL_REVISION:?set immutable FIDELITY_VLM_MODEL_REVISION}"

  if secret_has_key VEO_API_KEY; then
    : "${VEO_MODEL:=veo-3.1-fast-generate-preview}"
  fi
  if secret_has_key SEEDANCE_API_KEY; then
    : "${SEEDANCE_MODEL:?set the immutable Ark Seedance endpoint/model ID}"
  fi
}

wire_api() {
  validate_contract
  local env_args=(
    "FIDELITY_VLM_URL=$FIDELITY_VLM_URL"
    "FIDELITY_VLM_MODEL=$FIDELITY_VLM_MODEL"
    "FIDELITY_VLM_MODEL_REVISION=$FIDELITY_VLM_MODEL_REVISION"
    "FIDELITY_VLM_THRESHOLD=${FIDELITY_VLM_THRESHOLD:-0.88}"
  )
  secret_has_key VEO_API_KEY && env_args+=(
    "VEO_BASE_URL=${VEO_BASE_URL:-https://generativelanguage.googleapis.com/v1beta}"
    "VEO_MODEL=$VEO_MODEL"
  )
  secret_has_key SEEDANCE_API_KEY && env_args+=(
    "SEEDANCE_BASE_URL=${SEEDANCE_BASE_URL:-https://ark.cn-beijing.volces.com/api/v3}"
    "SEEDANCE_MODEL=$SEEDANCE_MODEL"
  )

  kubectl -n "$namespace" set env deployment/"$api_deployment" "${env_args[@]}"
  kubectl -n "$namespace" set env deployment/"$api_deployment" --from=secret/"$secret_name"
  kubectl -n "$namespace" rollout status deployment/"$api_deployment" --timeout=5m
  verify_api
}

verify_api() {
  require kubectl
  local env_json
  env_json="$(kubectl -n "$namespace" get deployment "$api_deployment" -o jsonpath='{.spec.template.spec.containers[0].env}')"
  for name in FIDELITY_VLM_URL FIDELITY_VLM_MODEL FIDELITY_VLM_MODEL_REVISION FIDELITY_VLM_THRESHOLD FIDELITY_VLM_API_KEY; do
    grep -q "\"name\":\"${name}\"" <<<"$env_json" || {
      echo "$api_deployment is missing $name" >&2
      exit 1
    }
  done
  grep -q "\"name\":\"$secret_name\"" <<<"$env_json" || {
    echo "provider credentials are not Secret references" >&2
    exit 1
  }
  echo "$api_deployment video-provider wiring verified (values not printed)"
}

unwire_api() {
  require kubectl
  kubectl -n "$namespace" set env deployment/"$api_deployment" \
    FIDELITY_VLM_URL- FIDELITY_VLM_MODEL- FIDELITY_VLM_MODEL_REVISION- \
    FIDELITY_VLM_THRESHOLD- FIDELITY_VLM_API_KEY- \
    VEO_BASE_URL- VEO_MODEL- VEO_API_KEY- \
    SEEDANCE_BASE_URL- SEEDANCE_MODEL- SEEDANCE_API_KEY-
  kubectl -n "$namespace" rollout status deployment/"$api_deployment" --timeout=5m
}

status() {
  require kubectl
  kubectl -n "$namespace" get secret "$secret_name" \
    -o go-template='secret={{.metadata.namespace}}/{{.metadata.name}} keys={{range $key, $_ := .data}}{{$key}} {{end}}{{"\n"}}'
  kubectl -n "$namespace" get deployment "$api_deployment" \
    -o go-template='deployment={{.metadata.name}} ready={{.status.readyReplicas}}/{{.spec.replicas}}{{"\n"}}'
}

case "${1:-}" in
  wire-api) wire_api ;;
  verify-api) verify_api ;;
  unwire-api) unwire_api ;;
  status) status ;;
  *) echo "usage: $0 {wire-api|verify-api|unwire-api|status}" >&2; exit 2 ;;
esac
