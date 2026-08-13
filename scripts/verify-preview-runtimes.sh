#!/usr/bin/env bash
set -euo pipefail

root=${1:-deploy/dev}
render=$(mktemp)
trap 'rm -f "$render"' EXIT
kubectl kustomize "$root" >"$render"

for runtime in hunyuan ltx musetalk; do
  grep -q "name: kwanni-$runtime" "$render"
  grep -q "app: kwanni-$runtime" "$render"
done

test "$(grep -c 'replicas: 0' "$render")" -ge 4
if grep -Eq '^kind: HTTPRoute$|^kind: Ingress$' "$render"; then
  echo 'preview runtimes must not render a public HTTPRoute or Ingress' >&2
  exit 1
fi
for key in HUNYUAN_API_KEY LTX_API_KEY MUSETALK_API_KEY; do
  grep -q "key: $key" "$render"
  if grep -Eq "value: .*${key}|${key}=.{4}" "$render"; then
    echo "$key must only be referenced from a Secret" >&2
    exit 1
  fi
done

grep -q 'port: 8091' "$render"
grep -q 'port: 8092' "$render"
grep -q 'port: 8093' "$render"
echo 'Preview runtime manifests: PASS'
