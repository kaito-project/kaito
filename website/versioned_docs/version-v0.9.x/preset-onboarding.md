---
title: Preset onboarding
---

This document describes how to add a new supported OSS model in KAITO. The process is designed to allow community users to initiate the request. KAITO maintainers will follow up and deal with managing the model images and guiding the code changes to set up the model preset configurations.

## Step 1: Make a proposal

This step is done by the requester. The requester should make a PR to describe the target OSS model following this [template](https://github.com/kaito-project/kaito/blob/main/docs/proposals/YYYYMMDD-model-template.md). The proposal status should be `provisional` in the beginning. KAITO maintainers will review the PR and decide to accept or reject the PR. The PR could be rejected if the target OSS model has low usage, or it has strict license limitations, or it is a relatively small model with limited capabilities.


## Step 2: Validate and test the model

This step is done by KAITO maintainers. Based on the information provided in the proposal, KAITO maintainers will download the model and test it using the specified runtime. The entire process is automated via GitHub actions when KAITO maintainers file a PR to add the model to the [supported\_models.yaml](https://github.com/kaito-project/kaito/blob/main/presets/workspace/models/supported_models.yaml).


## Step 3: Push model image to MCR

This step is done by KAITO maintainers. If the model license allows, KAITO maintainers will push the model image to MCR, making the image publicly available. This step is skipped if only private access is allowed for the model image. Once this step is done, KAITO maintainers will update the status of the proposal submitted in Step 1 to `ready to integrate`.

## Step 4: Add preset configurations

This step is done by the requester. The requester will work on a PR to register the model with preset configurations. The implementation involves several files and components. Below is a detailed guide using the GPT OSS model addition as an example.

### 4.1 Create the model package structure

Create a new package directory under `presets/workspace/models/` for your model family. For GPT OSS, this was `presets/workspace/models/gpt/`.

### 4.2 Implement the model.go file

Create a `model.go` file that implements the model interface. This file must include:

1. **Package registration** in the `init()` function using `plugin.KaitoModelRegister.Register()`
2. **Model constants** that match the names in `supported_models.yaml`
3. **Struct types** implementing the model interface with the following methods:
   - `GetInferenceParameters() *model.PresetParam` -- indicates the requirements and commands for running the model.
   - `SupportTuning() bool` -- whether or not the model supports fine tuning
   - `GetTuningParameters() *model.PresetParam` -- if the model supports fine tuning, specifies resource requirements, runtime config, tuning-specific parameters, and if not it returns `nil`.
   - `SupportDistributedInference() bool` -- whether or not the model can run on multiple nodes, set to true if the model cannot fit on a large node like H100. 

Example structure from GPT OSS:
```go
const (
    PresetGPT_OSS_20BModel = "gpt-oss-20b"
    PresetGPT_OSS_120BModel = "gpt-oss-120b"
)

type gpt_oss_20B struct{}
type gpt_oss_120B struct{}
```

### 4.3 Configure GPU memory requirements

In `GetInferenceParameters()`, specify the GPU memory requirements by checking the model specifications. These can typically be found on the website that provides the model. Hugging Face is a good place to start, and this page for [GPT-OSS 120B](https://huggingface.co/openai/gpt-oss-120b) indicates the required GPU memory.

- `DiskStorageRequirement`: Storage needed for model weights
- `TotalSafeTensorFileSize`: Total SafeTensor file size needed (e.g., "16Gi" for 20B, "80Gi" for 120B)
- `GPUCountRequirement`: Number of GPUs required
- `PerGPUMemoryRequirement`: Set to "0Gi" for models with native parallel support

### 4.4 Configure runtime parameters

Define runtime parameters for both Transformers and vLLM in the `RuntimeParam` struct:

**For Transformers runtime:**
```go
Transformers: model.HuggingfaceTransformersParam{
    BaseCommand:       "accelerate launch",
    ModelRunParams:    gptRunParams, // Model-specific parameters
}
```

**For vLLM runtime:**
```go
VLLM: model.VLLMParam{
    BaseCommand:    inference.DefaultVLLMCommand,
    ModelName:     PresetGPT_OSS_20BModel,
    ModelRunParams: gptRunParamsVLLM, // vLLM-specific parameters
}
```

The vLLM run parameters will likely have a `dtype` field as follows and may also require additional values.

```go
gptRunParamsVLLM = map[string]string{
    "dtype":             "float16",
}
```

**Special runtime parameters you may need**:
- `"trust_remote_code": ""` - For models that require remote code execution (like Phi models)
- `"allow_remote_files": ""` - For models that need to fetch additional files at runtime (like GPT OSS, DeepSeek)
- `"reasoning-parser": "deepseek_r1"` - For models with specific reasoning capabilities
- `"tool-call-parser": "model_specific"` - For models supporting function calling

### 4.5 Configure chat templates (when needed)

Some models require custom chat templates for proper formatting of chat-based interactions. If your model needs a specific chat template:

1. **Create a Jinja template file** in `/presets/workspace/inference/chat_templates/` following the naming pattern. For example:
   - `tool-chat-deepseekr1.jinja` for DeepSeek R1 models
   - `tool-chat-phi4-mini.jinja` for Phi-4 Mini models

2. **Reference the template in vLLM parameters**:
```go
modelRunParamsVLLM = map[string]string{
    "dtype":                   "float16",
    "chat-template":           "/workspace/chat_templates/tool-chat-modelname.jinja",
    "tool-call-parser":        "model_specific_parser",
    "enable-auto-tool-choice": "",
}
```

3. **When to add chat templates**:
   - Models that support function/tool calling
   - Models with specific conversation formatting requirements
   - Models that need custom reasoning parsers (like DeepSeek R1)

Refer to `/presets/workspace/inference/chat_templates/chat_template_guide.md` for detailed guidance on creating chat templates.

### 4.6 Configure tuning support (optional)

If your model supports fine-tuning, implement the `GetTuningParameters()` method and set `SupportTuning()` to return `true`:

```go
func (*modelName) GetTuningParameters() *model.PresetParam {
    return &model.PresetParam{
        Metadata:                  metadata.MustGet(PresetModelName),
        DiskStorageRequirement:    "90Gi",
        GPUCountRequirement:       "1",
        TotalSafeTensorFileSize: "16Gi",
        PerGPUMemoryRequirement:   "16Gi",
        RuntimeParam: model.RuntimeParam{
            Transformers: model.HuggingfaceTransformersParam{
                BaseCommand:      baseCommandPresetModelTuning,
                AccelerateParams: tuning.DefaultAccelerateParams,
            },
        },
        ReadinessTimeout:              time.Duration(30) * time.Minute,
        TuningPerGPUMemoryRequirement: map[string]int{"qlora": 16},
    }
}

func (*modelName) SupportTuning() bool {
    return true
}
```

If your model doesn't support tuning, simply return `nil` from `GetTuningParameters()` and `false` from `SupportTuning()`.

### 4.7 Set distributed inference support

Use `SupportDistributedInference()` to indicate whether the model can be distributed across multiple nodes:
- Return `false` for models that fit on a single node (like GPT OSS 20B)
- Return `true` for larger models that may benefit from distribution (like GPT OSS 120B) or cannot fit on a single node, like Llama 3.3 70B.

### 4.8 Add model entries to supported_models.yaml

Add your model entries to `/presets/workspace/models/supported_models.yaml`:
```yaml
# GPT-OSS
- name: gpt-oss-20b
  type: text-generation
  version: https://huggingface.co/openai/gpt-oss-20b/commit/...
  runtime: tfs
  downloadAtRuntime: true
  tag: 0.0.1
```

### 4.9 Create example workspace files

Create example workspace YAML files in `/examples/inference/` (e.g., `kaito_workspace_gpt_oss_20b.yaml`):
```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: workspace-gpt-oss-20b
resource:
  instanceType: "Standard_NC24ads_A100_v4"  # Choose appropriate GPU instance
  labelSelector:
    matchLabels:
      apps: gpt-oss-20b
inference:
  preset:
    name: "gpt-oss-20b"
```

Choose instance types based on GPU memory requirements. Some examples include:

- For models requiring ~20-24GB: `Standard_NC24ads_A100_v4` (A100 with 80GB)
- For models requiring 80GB: `Standard_NC24ads_A100_v4` (A100 with 80GB)

### 4.10 Add a README for the model family

Create `/presets/workspace/models/{model_family}/README.md` following the standard format used by other models:
```markdown
## Supported Models
| Model name | Model source | Sample workspace | Kubernetes Workload | Distributed inference |
|------------|--------------|------------------|--------------------|--------------------|
| gpt-oss-20b | [openai](https://huggingface.co/openai/gpt-oss-20b) | [link](../../../../../examples/inference/kaito_workspace_gpt_oss_20b.yaml) | Deployment | false |
```

## Step 5: Add E2E tests

This step is done by the requester. Add comprehensive test coverage for both Transformers and vLLM runtimes.

### 5.1 Add tests to preset_test.go

Add test functions to `/test/e2e/preset_test.go`. For example, the test for GPT-OSS 20B is as follows

```go
It("should create a gpt-oss-20b workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
    numOfNode := 1
    workspaceObj := createGPTOss20BWorkspaceWithPresetPublicMode(numOfNode)

    defer cleanupResources(workspaceObj)
    time.Sleep(30 * time.Second)

    validateCreateNode(workspaceObj, numOfNode)
    validateResourceStatus(workspaceObj)

    time.Sleep(30 * time.Second)

    validateAssociatedService(workspaceObj)
    validateInferenceConfig(workspaceObj)

    validateInferenceResource(workspaceObj, int32(numOfNode), false)

    validateWorkspaceReadiness(workspaceObj)
})
```

### 5.2 Add vLLM tests to preset_vllm_test.go

If the model supports vLLM, add a corresponding vLLM test to `/test/e2e/preset_vllm_test.go` following the same pattern. The test should be similar to this:

```go
It("should create a gpt-oss-20b workspace with preset public mode successfully", utils.GinkgoLabelA100Required, func() {
    numOfNode := 1
    workspaceObj := createGPTOss20BWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

    defer cleanupResources(workspaceObj)
    time.Sleep(30 * time.Second)

    validateCreateNode(workspaceObj, numOfNode)
    validateResourceStatus(workspaceObj)

    time.Sleep(30 * time.Second)

    validateAssociatedService(workspaceObj)
    validateInferenceConfig(workspaceObj)

    validateInferenceResource(workspaceObj, int32(numOfNode), false)

    validateWorkspaceReadiness(workspaceObj)
    validateModelsEndpoint(workspaceObj)
    validateCompletionsEndpoint(workspaceObj)
    validateGatewayAPIInferenceExtensionResources(workspaceObj)
})
```

### 5.3 Update test configuration files

Update `/.github/e2e-preset-configs.json` with the new model information and command. For GPT-OSS 20B, the entry might look like this:

```json
{
    "name": "gpt-oss-20b",
    "node-count": 1,
    "node-vm-size": "Standard_NC24ads_A100_v4",
    "node-osdisk-size": 120,
    "OSS": true,
    "loads_adapter": false,
    "node_pool": "gptoss20b",
    "runtimes": {
        "hf": {
            "command": "accelerate launch --num_processes 1 --num_machines 1 --machine_rank 0 --gpu_ids all /workspace/tfs/inference_api.py --pipeline text-generation --torch_dtype auto --allow_remote_files",
            "gpu_count": 1
        }
    }
}
```

## Step 6: Update additional configuration files

### 6.1 Update Helm chart configuration

Update `/charts/kaito/workspace/templates/supported-models-configmap.yaml` to include the new model in the Kubernetes ConfigMap that the workspace controller uses.

### 6.2 Update documentation

Consider updating the following documentation files:
- `/website/docs/gpu-benchmarks.md`: Add performance benchmarks if available
- Any model-specific documentation that references supported models

## Summary of files to modify for a new preset

When adding a new model preset, you will need to create or modify the following files:

**Core implementation:**
1. `/presets/workspace/models/{model_family}/model.go` - Main model implementation
2. `/presets/workspace/models/{model_family}/README.md` - Documentation
3. `/presets/workspace/models/supported_models.yaml` - Model registry

**Examples and documentation:**
4. `/examples/inference/kaito_workspace_{model_name}.yaml` - Example workspace files

**Testing:**
5. `/test/e2e/preset_test.go` - Transformers runtime tests
6. `/test/e2e/preset_vllm_test.go` - vLLM runtime tests
7. `/.github/e2e-preset-configs.json` - CI test configuration

**Deployment configuration:**
8. `/charts/kaito/workspace/templates/supported-models-configmap.yaml` - Helm chart ConfigMap

**Chat templates (when needed):**
9. `/presets/workspace/inference/chat_templates/{model_name}.jinja` - Custom chat templates for models requiring specific formatting

**Optional documentation updates:**
10. `/website/docs/gpu-benchmarks.md` - Performance documentation
11. Other relevant documentation files

In the same PR, or a separate PR, the status of the proposal status should be updated to `integrated`.

After all the above steps are completed, a new model becomes available in KAITO. The comprehensive implementation ensures that the model works with both Transformers and vLLM runtimes, has proper test coverage, and follows KAITO's established patterns for consistency and maintainability.
