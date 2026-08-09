# ShotVL visual-QC runtime (Development profile)

This is a KWANNI-owned, optional runtime. It is deliberately outside KSys and
KDeploy. Applying it creates a private ClusterIP and a zero-replica Deployment;
it does not make ShotVL resident and does not add an Ingress, Gateway API route,
NodePort, or LoadBalancer.

ShotVL is an advisory cinematography critic (shot size, framing, angle, lens,
lighting, composition, and movement). It is not evidence that product text,
logos, colors, or geometry are exact, so product fidelity still requires the
canonical product bible and human review.

## Immutable inputs

- Model: `Vchitect/ShotVL-7B`
- Model revision: `fd8c356e8fd8d116417dddb6acc49fbf45f9abbc`
- Runtime: `nvcr.io/nvidia/vllm:26.05.post1-py3`
- Runtime index digest:
  `sha256:94e21552f644e0c1627464ba89d2f7a4ce7442e196f72afa0bb5d7fba23cbb03`
- ARM64 child digest:
  `sha256:9204569b17ee4c0eff75194b8e6e458479c8aee18953b5ab9cf359fcdac659e2`

The public model needs no Hugging Face token. Pre-fetch stores the immutable
snapshot below `/opt/local/models/shotvl`; the serving pod is offline and mounts
that cache read-only.

## Install and operate

Create one random key in a root-owned file and create both Kubernetes Secrets
from that file. Do not put it in shell history, `.env.dev`, Git, or command-line
literals:

```bash
umask 077
openssl rand -base64 48 > /secure/path/shotvl-api-key
kubectl -n dev create secret generic kwanni-shotvl-auth \
  --from-file=SHOTVL_API_KEY=/secure/path/shotvl-api-key \
  --dry-run=client -o yaml | kubectl apply -f -
```

The `dev-llm-api` application Secret must expose the same protected file as
`SHOTVL_API_KEY`. Its non-secret environment is:

```text
SHOTVL_URL=http://kwanni-shotvl.dev.svc.cluster.local:8000/v1
SHOTVL_MODEL=shotvl-7b
SHOTVL_MODEL_REVISION=fd8c356e8fd8d116417dddb6acc49fbf45f9abbc
SHOTVL_THRESHOLD=0.75
```

Then run, from this repository:

```bash
sudo scripts/shotvl-runtime.sh prefetch
scripts/shotvl-runtime.sh install  # remains replicas=0
scripts/shotvl-runtime.sh wire-api # controlled Alpha: patch only ShotVL env
scripts/shotvl-runtime.sh up       # load only for a visual-QC window
scripts/shotvl-runtime.sh status
scripts/shotvl-runtime.sh down     # release the vLLM allocation
```

Scale up before exports that should receive advisory QC, then scale down after
the Mongo visual-QC queue drains. An unavailable/cold/failed ShotVL remains
non-blocking: export/download/posting continue, and a human can override the
advisory result. Qwen3.6 remains resident throughout.

`wire-api` is the explicit Development/controlled-Alpha bridge after a clean
KDeploy install. It patches only the five `SHOTVL_*` variables on the existing
`dev-llm-api` Deployment and verifies that the API key remains a Secret
reference without printing values. It does not take ownership of the full
KDeploy-generated Deployment. Re-run `wire-api` after KDeploy recreates that
Deployment. `unwire-api` removes only those five variables.

## Scheduler and resource boundary

The application claims at most one Mongo-leased visual-QC job globally and this
runtime additionally sets `--max-num-seqs 1`. It caps vLLM to 25% GPU-memory
utilization, 16K model context, 12 resized keyframes, one GPU, 8 CPU, and 48 GiB
host/unified memory. These are conservative starting bounds, not measured DGX
Spark capacity guarantees. Validate Qwen throughput and unified-memory pressure
on the target before raising them.

There is deliberately no cross-runtime admission controller in Mission Zero:
the Mongo lease serializes ShotVL jobs, but it cannot pause requests already
reaching the host-owned Qwen service. `up` therefore does not stop or unload
Qwen. Until a shared GPU arbiter exists, the operator must open ShotVL only in a
quiet interactive window and confirm the Qwen queue is idle using the existing
host runtime's metrics. If deterministic interactive latency is required, leave
ShotVL at zero replicas and use human review. Do not automate `systemctl stop`,
`SIGSTOP`, or model unloading from this project-owned script.

With 12 images at a bounded 401,408 pixels each, 16K is bounded by design. If the
processor rejects a real request, reduce keyframe count/resolution in the
application based on evidence; do not increase context or GPU utilization while
Qwen is resident without a concurrent-load benchmark.

The NetworkPolicy admits TCP/8000 only from pods labelled `app=dev-llm-api` and
denies runtime egress. vLLM's key protects `/v1`, but vLLM documents that some
operational endpoints are not covered by API-key authentication; network
isolation is therefore mandatory. This assumes the cluster network-policy
controller is enabled and must be verified on point-DGX before Alpha use.

The pinned NGC image's ARM64 config runs as root. Pre-fetch intentionally makes
the host cache root-owned mode `0750`; the serving mount is read-only, Linux
capabilities are dropped, privilege escalation is disabled, and the pod has no
service-account token. Confirm the point-DGX node label is
`kubernetes.io/arch=arm64` and that the NVIDIA device plugin advertises one GPU.
Both the NVIDIA device-plugin DaemonSet and this Deployment must use the K3s
`runtimeClassName: nvidia`; a Running plugin under default runc can report no
allocatable GPU even though host `nvidia-smi` succeeds.

Registry inspection of the pinned ARM64 child manifest resolved config digest
`sha256:46591c6e4a018d8d197fa246b1e3d682c907654aab4e9402302abb3e6a7dd916`.
Its entrypoint is `/opt/nvidia/nvidia_entrypoint.sh` with no default command, so
Kubernetes `args: [vllm, serve, ...]` and the pre-fetch `python3 -c ...` follow
the same command-forwarding contract NVIDIA documents for `docker run IMAGE
vllm serve MODEL`; neither duplicates a `vllm` image entrypoint.

## Validation (no live mutation)

```bash
scripts/verify-shotvl-runtime.sh
kubectl kustomize deploy/dev/shotvl >/tmp/kwanni-shotvl.yaml
kubectl kustomize deploy/dev >/tmp/kwanni-development.yaml
kubectl apply --dry-run=client -f /tmp/kwanni-shotvl.yaml
```

Do not deploy or pre-fetch from CI. Those operations are explicit point-DGX
operator actions after review.
