#!/usr/bin/env bash
set -euo pipefail

command -v grep >/dev/null 2>&1 || { echo "required command missing: grep" >&2; exit 1; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="$root/deploy/dev/shotvl/deployment.yaml"
root_kustomization="$root/deploy/dev/kustomization.yaml"
service="$root/deploy/dev/shotvl/service.yaml"
policy="$root/deploy/dev/shotvl/network-policy.yaml"

grep -q 'replicas: 0' "$manifest"
grep -q 'max-num-seqs' "$manifest"
grep -q 'gpu-memory-utilization' "$manifest"
grep -q 'HF_HUB_OFFLINE' "$manifest"
grep -q 'VLLM_API_KEY' "$manifest"
grep -Eq 'image: .*@sha256:[0-9a-f]{64}$' "$manifest"
grep -q 'kubernetes.io/arch: arm64' "$manifest"
grep -q 'fd8c356e8fd8d116417dddb6acc49fbf45f9abbc' "$manifest"
grep -q 'type: ClusterIP' "$service"
grep -q 'app: dev-llm-api' "$policy"
grep -q 'egress: \[\]' "$policy"
grep -q -- '- shotvl' "$root_kustomization"
grep -q 'SHOTVL_URL=http://kwanni-shotvl.dev.svc.cluster.local:8000/v1' "$root/scripts/shotvl-runtime.sh"
grep -q -- '--from=secret/kwanni-shotvl-auth' "$root/scripts/shotvl-runtime.sh"

if grep -REn 'kind: (Ingress|HTTPRoute)|type: (NodePort|LoadBalancer)' "$root/deploy/dev/shotvl"; then
  echo "ShotVL must not have a public route" >&2
  exit 1
fi

if grep -REn '(ghp_|github_pat_|hf_[A-Za-z0-9]{16,}|api-key: [^<])' "$root/deploy/dev/shotvl" "$root/scripts/shotvl-runtime.sh"; then
  echo "possible credential committed in ShotVL runtime files" >&2
  exit 1
fi

echo "ShotVL runtime static policy checks passed"
