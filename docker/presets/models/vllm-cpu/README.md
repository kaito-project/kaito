# vLLM CPU runtime image

`kaito-base-vllm-cpu` packages the upstream vLLM CPU runtime with KAITO's
existing vLLM scripts and chat templates. It contains no model weights and
supports **linux/amd64 only**. The upstream vLLM version must match
`presets/workspace/dependencies/requirements.txt`; do not install that GPU
requirements file into this image.

Build locally without publishing:

```sh
make docker-build-kaito-base-vllm-cpu \
  REGISTRY=local KAITO_BASE_IMG_TAG=0.0.1 OUTPUT_TYPE=type=docker
```

The image retains upstream's `vllm serve` entrypoint. For example, on a supported
CPU host (this downloads model weights):

```sh
docker run --rm -p 8000:8000 --shm-size=4g \
  -e VLLM_CPU_KVCACHE_SPACE=4 \
  local/kaito-base-vllm-cpu:0.0.1 Qwen/Qwen2.5-0.5B-Instruct \
  --dtype bfloat16 --max-model-len 2048
```

Publication uses the existing runtime-image workflow and the `base-vllm-cpu`
entry in `presets/workspace/models/base_images.yaml`. This image has its own tag
history; adding it does not change the existing GPU `base` image tag. The shared
E2E setup builds it alongside the GPU image when `build_kaito_base_image` is true;
this does not add a CPU inference E2E test.

This is image/build plumbing only. It does not enable CPU preset inference in
KAITO, change CPU scheduling or validation, or add a transformers CPU runtime.
The packaged KAITO wrapper is unchanged from `main`; CPU-specific wrapper and
controller behavior is separate follow-up work.
