# Model selection and GPU sizing

Use this reference when the requested model, access requirements, or resource sizing is unresolved.

## Presets

Use `kubectl kaito models describe '<model-name>'` for a known preset, or `kubectl kaito models list` to discover candidates. Consult installed help before requesting detailed or structured output: flag types can differ from examples in published documentation.

The catalog is fetched from the KAITO repository, not discovered from the running operator. Check compatibility with the user's KAITO version. Model details may omit GPU requirements; absence is not evidence that a model fits.

Source: [kubectl plugin model documentation](https://github.com/kaito-project/kaito-kubectl-plugin/blob/main/docs/models.md).

## Hugging Face models

If the exact model ID is already known, inspect that model rather than running a broad search. Otherwise query the official Hub API with URL-encoded search parameters:

```bash
curl --fail --silent --show-error --get \
  'https://huggingface.co/api/models' \
  --data-urlencode 'search=<model-query>' \
  --data-urlencode 'filter=text-generation' \
  --data-urlencode 'sort=downloads' \
  --data-urlencode 'direction=-1' \
  --data-urlencode 'limit=5'
```

Replace the search placeholder with a safely quoted query. Use the selected model's full `org/model` ID; inspect its official publisher, architecture, precision/quantization, license, and access requirements. Download counts and task tags aid discovery but do not establish trust or runtime compatibility. Check the architecture against the deployed KAITO/vLLM version rather than promising support for every Hub model.

For gated/private models, the user needs appropriate Hub access and an existing Kubernetes secret in the deployment namespace. Ask for its name, never its token. A public, ungated model does not require a secret solely because it comes from Hugging Face. If metadata cannot be retrieved, report the lookup failure and keep compatibility/access assumptions explicit.

Sources: [Hub API](https://huggingface.co/docs/hub/api), [KAITO custom models](https://github.com/kaito-project/kaito/blob/main/website/docs/custom-model.md).

## GPU resources

Start with the user's cloud, region, available hardware, and provisioning mode. Use current provider specifications and model/runtime requirements; do not default non-Azure users to Azure SKUs.

Weight memory depends on parameter count and precision; it is not the entire VRAM budget. Allow for KV cache, context length, concurrency, runtime overhead, and supported parallelism. Treat sizing as an estimate, not a guarantee based on parameter-count thresholds alone. Distinguish GPUs per node from node count and serving replicas.

Prefer a documented compatible configuration or KAITO's memory estimator over a static model-size-to-SKU table. If required metadata, quota, or hardware availability is unknown, state that before recommending provisioning.

Source: [KAITO memory estimator](https://github.com/kaito-project/kaito/blob/main/website/docs/memory-estimator.md).
