# Minimal RunAI image

Build a CPU-only image containing the RunAI model streamer and Azure backend:

```bash
hack/build-runai/build.sh
```

The default image is `runai-model-streamer:minimal`. Override it with:

```bash
IMAGE=example.azurecr.io/runai-model-streamer:minimal hack/build-runai/build.sh
```

The build uses pinned official RunAI wheels by default. To test locally built
RunAI wheels, copy the common and Azure wheels into `hack/build-runai/wheels/`:

```text
runai_model_streamer-<version>-py3-none-manylinux2014_x86_64.whl
runai_model_streamer_azure-<version>-py3-none-manylinux2014_x86_64.whl
```

Local wheels replace the official RunAI packages after their runtime
dependencies are installed. The final image runs as UID/GID `65532` and starts
with an import and version smoke test.

The image also contains the DACS warmer at
`/usr/local/bin/dacs-model-warmer.py`. KAITO overrides the image entrypoint to
run this script when `cache.providers.dacs.modelWarmerImage` is configured.
Before streaming, the warmer waits until `CACHE_DISCOVERY_URL` and
`CACHE_SERVER_PORT` accept a TCP connection so a cold-node cache startup does
not cause the warm to fall back to remote storage.
