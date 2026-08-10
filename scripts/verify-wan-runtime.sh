#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
rendered=$(mktemp)
trap 'unlink "$rendered" 2>/dev/null || true' EXIT
kubectl kustomize "$root/deploy/dev/wan" >"$rendered"

grep -q 'name: kwanni-wan' "$rendered"
grep -q 'replicas: 0' "$rendered"
grep -q 'type: Recreate' "$rendered"
grep -q 'runtimeClassName: nvidia' "$rendered"
grep -q 'nvidia.com/gpu: "1"' "$rendered"
grep -q 'secretKeyRef:' "$rendered"
grep -q 'name: kwanni-wan-auth' "$rendered"
if grep -Eq 'kind: (HTTPRoute|Ingress)|type: (NodePort|LoadBalancer)' "$rendered"; then
  echo 'Wan runtime must remain private' >&2
  exit 1
fi
grep -q 'policyTypes:' "$rendered"
grep -q 'app: dev-llm-api' "$rendered"
grep -q '921dbaf3f1674a56f47e83fb80a34bac8a8f203e' "$root/deploy/dev/wan/README.md"
grep -q 'source_revision=42bf4cfaa384bc21833865abc2f9e6c0e67233dc' "$root/scripts/wan-runtime.sh"
grep -q 'stdout=subprocess.DEVNULL' "$root/deploy/dev/wan/runtime/server.py"
grep -q 'runpy.run_path' "$root/deploy/dev/wan/runtime/launcher.py"
grep -q 'from .textimage2video import WanTI2V' "$root/deploy/dev/wan/runtime/wan_init.py"
echo 'Wan runtime policy: PASS'
