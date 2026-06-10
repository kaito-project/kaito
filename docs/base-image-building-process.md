# KAITO Base Image Building Process

This document describes how KAITO builds and distributes the **base image** — the
container image that ships the inference runtime (vLLM / transformers) and CUDA
stack with no model weights baked in.

## Source of Truth: supported_models.yaml

The `base` entry in
[`presets/workspace/models/supported_models.yaml`](../presets/workspace/models/supported_models.yaml)
is the single source of truth for the base image:

```yaml
- name: base
  type: text-generation
  runtime: tfs
  tag: 0.4.0                       # default base image tag
  baseImageOverrides:              # optional per-GPU-family tag pins
    - tag: "0.3.1"
      reason: "..."
      matchGPUModels: ["A10", "M60"]
```

- `tag` is the default base image tag published to the registry and consumed by
  every workspace whose GPU model does not match an override.
- `baseImageOverrides` pins specific GPU model families to an alternate tag (see
  the next section).
- A `Tag history` comment block in the same entry records what changed in each
  tag (vLLM / torch / CUDA bumps, CVE fixes, etc.).

Changing the `base` entry's `tag` is what triggers a new base image build.

## Pinning Legacy GPU SKUs to an Older Base Image

Different Azure GPU SKU families ship different NVIDIA driver lines. NV-series
VMs (e.g. A10, M60) install the NVIDIA **GRID** driver, which lags behind the
**CUDA** driver installed on NC/ND-series VMs (e.g. A100, H100). As a result, a
base image built against a newer CUDA toolkit can be incompatible with the driver
on an older SKU. For example, base image `0.4.0` (vLLM 0.22.1 / torch 2.11 /
CUDA 13) requires driver 580+, but NV-series A10 SKUs currently ship driver
550.x (CUDA 12.4).

To handle this, the `base` entry in
[`supported_models.yaml`](../presets/workspace/models/supported_models.yaml)
supports an optional `baseImageOverrides` list that pins specific GPU model
families to an alternate tag:

```yaml
- name: base
  type: text-generation
  runtime: tfs
  tag: 0.4.0                       # default tag (modern SKUs)
  baseImageOverrides:
    - tag: "0.3.1"                 # legacy tag for the matching families
      reason: "NV-series (A10/M60) ship NVIDIA GRID driver 550.x (CUDA 12.4); base image 0.4.0 (CUDA 13) needs driver 580+"
      matchGPUModels: ["A10", "M60"]
```

How it works:
- At pod-spec generation time, the controller resolves the workspace's GPU model
  (from the SKU under node auto-provisioning, or from the node's `nvidia.com/*`
  labels under bring-your-own-nodes) and selects the pinned tag if a family
  matches. This applies to both **inference** and **tuning** pods.
- Matching is **token-based and case-insensitive**: the GPU model string is split
  on spaces, hyphens, and underscores, and a family matches only if it equals one
  of those whole tokens. This makes `A10` match `NVIDIA A10` and
  `NVIDIA-A10` but **not** `NVIDIA A100`.
- The first matching override wins. A workspace whose GPU model matches no
  override (or whose GPU config is unknown) uses the default `tag`.
- The auto-upgrade controller resolves each workspace's desired image
  individually, so a pinned legacy workspace is never drifted toward the default
  tag and is never auto-upgraded past its pinned version.

To build a pinned legacy tag, build the base image at that tag the same way as
any other base image (see the build process below) and ensure it exists in the
target registry.

## Build Process

The base image is built from
[`docker/presets/models/tfs/Dockerfile`](../docker/presets/models/tfs/Dockerfile)
with two build arguments:

- `VERSION` — the base image tag (e.g. `0.4.0`)
- `MODEL_TYPE=text-generation`

### Building locally

```bash
make docker-build-kaito-base \
  REGISTRY=<your-registry> \
  KAITO_BASE_IMG_TAG=<tag> \
  OUTPUT_TYPE=type=registry
```

This produces `<REGISTRY>/kaito-base:<tag>`.

### CI: when the base image is (re)built

KAITO only builds the base image when its tag is missing from the target
registry, to avoid redundant builds.

- **Pull requests**: `.github/determine_missing_preset_images.py` is run and, if
  the `base` entry's tag is missing from the registry, `BUILD_KAITO_BASE_IMAGE`
  is set to `true`. The `e2e-base-setup` action then builds and pushes
  `$REGISTRY/kaito-base:$VERSION` to the temporary PR registry and overrides the
  `registry` and `tag` of the `base` entry in `supported_models.yaml` so tests
  exercise the freshly built image. It also verifies that
  `presets/workspace/models/vllm_model_arch_list.txt` matches the architectures
  reported by the new image.
- **Official release (post-merge)**: the same smart tag check runs against the
  official registry; the base image is built and pushed only when its tag does
  not already exist there.

### Pinned legacy tags

When `baseImageOverrides` pins a family to an older tag (e.g. `0.3.1`), that tag
must also exist in the registry. Build it the same way as any other base image
tag (set `KAITO_BASE_IMG_TAG` / `VERSION` to the pinned value) and push it to the
target registry.
