// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package model

import (
	"fmt"
	"maps"
	"path"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

type Model interface {
	// GetInferenceParameters returns the preset inference parameters for the model.
	GetInferenceParameters() *PresetParam

	// GetTuningParameters returns the preset tuning parameters for the model.
	GetTuningParameters() *PresetParam

	// SupportDistributedInference checks if the model supports distributed inference.
	SupportDistributedInference() bool

	// SupportTuning checks if the model supports tuning.
	SupportTuning() bool
}

// RuntimeName is LLM runtime name.
type RuntimeName string

const (
	RuntimeNameHuggingfaceTransformers RuntimeName = "transformers"
	RuntimeNameVLLM                    RuntimeName = "vllm"

	DefaultTuningMainFile = "/workspace/tfs/fine_tuning.py"
	ConfigfileNameVLLM    = "inference_config.yaml"

	// PortRayCluster is the default port for communication between the head and worker nodes in a Ray cluster.
	PortRayCluster = 6379
)

// Metadata defines the metadata for a model.
type Metadata struct {
	// Name is the name of the model, which serves as a unique identifier.
	// It is used to register the model information and retrieve it later.
	Name string `yaml:"name"`

	// ModelType is the type of the model, which indicates the kind of model
	// it is. Currently, the only supported types are "text-generation" and
	// "llama2-completion" (deprecated).
	ModelType string `yaml:"type"`

	// Version is the version of the model. It is a URL that points to the
	// model's huggingface page, which contains the model's repository ID
	// and revision ID, e.g. https://huggingface.co/mistralai/Mistral-7B-v0.3/commit/d8cadc02ac76bd617a919d50b092e59d2d110aff.
	Version string `yaml:"version"`

	// Runtime is the runtime environment in which the model operates.
	// Currently, the only supported runtime is "tfs".
	Runtime string `yaml:"runtime"`

	// DownloadAtRuntime indicates whether the model should be downloaded
	// at runtime. If set to true, the model will be downloaded when the
	// model deployment is created, and the container image will always be
	// the KAITO base image. If set to false, a container image whose name
	// contains the model name will be used, in which the model weights are baked.
	// +optional
	DownloadAtRuntime bool `yaml:"downloadAtRuntime,omitempty"`

	// DownloadAuthRequired indicates whether the model requires authentication to download.
	// +optional
	DownloadAuthRequired bool `yaml:"downloadAuthRequired,omitempty"`

	// Tag is the tag of the container image used to run the model.
	// If the model uses the KAITO base image, the tag field can be ignored
	// +optional
	Tag string `yaml:"tag,omitempty"`

	// Registry is the container registry where the model is stored.
	// If this is empty, os.Getenv("PRESET_REGISTRY_NAME") will be used.
	// +optional
	Registry string `yaml:"registry,omitempty"`

	// Deprecated indicates if the model is deprecated.
	// +optional
	Deprecated bool `yaml:"deprecated,omitempty"`

	// Architectures specifies the supported architectures for the model
	// This field is only for best effort supported vLLM models.
	// +optional
	Architectures []string `yaml:"architectures,omitempty"`

	// DType specifies the data type used by the model (e.g., "bfloat16", "float16", "float32").
	// This field is only for best effort supported vLLM models.
	// +optional
	DType string `yaml:"dtype,omitempty"`

	// ModelFileSize is the size of the model file, example: 14Gi.
	// This field is only for best effort supported vLLM models.
	// +optional
	ModelFileSize string `yaml:"modelFileSize,omitempty"`

	// DiskStorageRequirement is the disk storage requirement for the model, example: 90Gi.
	// This field is only for best effort supported vLLM models.
	// +optional
	DiskStorageRequirement string `yaml:"diskStorageRequirement,omitempty"`

	// BytesPerToken is the number of bytes used to represent each token in the model.
	// This field is only for best effort supported vLLM models.
	// +optional
	BytesPerToken int `yaml:"bytesPerToken,omitempty"`

	// ModelTokenLimit is the maximum number of tokens (context window) supported by the model.
	// This field is only for best effort supported vLLM models.
	// +optional
	ModelTokenLimit int `yaml:"modelTokenLimit,omitempty"`

	// ToolCallParser specifies the parser used for tool calls within the model.
	// This field is only for best effort supported vLLM models.
	// +optional
	ToolCallParser string `yaml:"toolCallParser,omitempty"`

	// ReasoningParser specifies the parser used for reasoning within the model.
	// This field is only for best effort supported vLLM models.
	// +optional
	ReasoningParser string `yaml:"reasoningParser,omitempty"`

	// ChatTemplate is the chat template file name used for chat models.
	// This field is only for best effort supported vLLM models.
	// +optional
	ChatTemplate string `yaml:"chatTemplate,omitempty"`

	// AllowRemoteFiles indicates whether the model allows loading remote files.
	// This field is only for best effort supported vLLM models.
	// +optional
	AllowRemoteFiles bool `yaml:"allowRemoteFiles,omitempty"`

	// QuantMethod specifies the weight quantization method used by the model
	// (e.g., "awq", "gptq"). Maps to quantization_config.quant_method in the
	// model's HuggingFace config.json.
	// +optional
	QuantMethod string `yaml:"quantMethod,omitempty"`

	// QuantBits specifies the number of bits used for weight quantization
	// (e.g., 4 for 4-bit AWQ). Maps to quantization_config.bits in the
	// model's HuggingFace config.json.
	// +optional
	QuantBits int `yaml:"quantBits,omitempty"`
}

// Validate checks if the Metadata is valid.
func (m *Metadata) Validate() error {
	// Some models requiring authentication may not have a version URL, so we allow it to be empty until
	// we remove support for preset models requiring authentication.
	if m.Version == "" {
		return nil
	}

	_, _, err := utils.ParseHuggingFaceModelVersion(m.Version)
	return err
}

// PresetParam defines the preset inference parameters for a model.
type PresetParam struct {
	Metadata

	DiskStorageRequirement string // Disk storage requirements for the model.
	// DiskStorageRequirement is calculated as:
	// (TotalSafeTensorFileSize × 2.5 + 48) rounded up to the next multiple of 10.
	// This formula accounts for model weights, optimization files, and runtime overhead.
	// Example: For a 14Gi model, calculation is: 14 × 2.5 + 48 = 83, rounded up to 90Gi.

	ImageAccessMode               string         // Defines where the Image is Public or Private.
	GPUCountRequirement           string         // Number of GPUs required for the Preset. Used for inference.
	TotalSafeTensorFileSize       string         // Total SafeTensor file size for the Preset. Used for inference.
	TuningPerGPUMemoryRequirement map[string]int // Min GPU memory per tuning method (batch size 1). Used for tuning.
	BytesPerToken                 int            // Number of bytes per token for the model. It is calculated by 2 * hidden_layers * kv_heads * head_dim (hidden_size/num_attemtion_numbers) * dtype_size
	ModelTokenLimit               int            // Maximum number of tokens (context window) supported by the model. Maps to 'max_position_embeddings' in the model's Hugging Face config.json.

	// To determine TotalSafeTensorFileSize and BytesPerToken values for a new model,
	// run the presets/workspace/generator/preset_generator.py script
	// with the model's Hugging Face repository ID as an argument.

	// AttnType specifies the attention implementation (e.g., MHA, GQA, MLA).
	// Calculated by the preset generator based on model config.
	AttnType string `yaml:"attn_type,omitempty"`

	RuntimeParam

	// ReadinessTimeout defines the maximum duration for creating the workload.
	// This timeout accommodates the size of the image, ensuring pull completion
	// even under slower network conditions or unforeseen delays.
	ReadinessTimeout time.Duration
}

// RuntimeParam defines the llm runtime parameters.
type RuntimeParam struct {
	Transformers HuggingfaceTransformersParam
	VLLM         VLLMParam
	// Disable the tensor parallelism
	DisableTensorParallelism bool
}

type HuggingfaceTransformersParam struct {
	BaseCommand       string            // The initial command (e.g., 'accelerate launch') used in the command line.
	AccelerateParams  map[string]string // Parameters for configuring the accelerate command.
	InferenceMainFile string            // The main file for inference.
	ModelRunParams    map[string]string // Parameters for running the model training/inference.
	// The model name used in the OpenAI serving API.
	ModelName string
	// Tag is the ORAS image tag for pre-built model weights.
	Tag string
}

type VLLMParam struct {
	RayLeaderBaseCommand string
	RayLeaderParams      map[string]string
	RayWorkerBaseCommand string
	RayWorkerParams      map[string]string
	// BaseCommand is the command used to start the inference server.
	BaseCommand string
	// The model name used in the openai serving API.
	// see https://platform.openai.com/docs/api-reference/chat/create#chat-create-model.
	ModelName string
	// Parameters for running the model training/inference.
	ModelRunParams map[string]string
	// Indicates if vllm supports LoRA (Low-Rank Adaptation) for this model.
	// doc: https://docs.vllm.ai/en/latest/models/supported_models.html#text-generation-task-generate
	DisallowLoRA bool
}

func (p *PresetParam) DeepCopy() *PresetParam {
	if p == nil {
		return nil
	}
	out := new(PresetParam)
	*out = *p
	out.RuntimeParam = p.RuntimeParam.DeepCopy()
	out.TuningPerGPUMemoryRequirement = maps.Clone(p.TuningPerGPUMemoryRequirement)
	return out
}

func (rp *RuntimeParam) DeepCopy() RuntimeParam {
	if rp == nil {
		return RuntimeParam{}
	}
	out := *rp
	out.Transformers = rp.Transformers.DeepCopy()
	out.VLLM = rp.VLLM.DeepCopy()
	return out
}

func (h *HuggingfaceTransformersParam) DeepCopy() HuggingfaceTransformersParam {
	if h == nil {
		return HuggingfaceTransformersParam{}
	}
	out := *h
	out.AccelerateParams = maps.Clone(h.AccelerateParams)
	out.ModelRunParams = maps.Clone(h.ModelRunParams)
	return out
}

func (v *VLLMParam) DeepCopy() VLLMParam {
	if v == nil {
		return VLLMParam{}
	}
	out := *v
	out.RayLeaderParams = maps.Clone(v.RayLeaderParams)
	out.RayWorkerParams = maps.Clone(v.RayWorkerParams)
	out.ModelRunParams = maps.Clone(v.ModelRunParams)
	return out
}

// RuntimeContext defines the runtime context for a model.
type RuntimeContext struct {
	RuntimeName          RuntimeName
	GPUConfig            *sku.GPUConfig
	ConfigVolume         *corev1.VolumeMount
	SKUNumGPUs           int
	NumNodes             int
	WorkspaceMetadata    metav1.ObjectMeta
	DistributedInference bool
	MaxModelLen          int // max-model-len parameter for vLLM
	RuntimeContextExtraArguments
}

type RuntimeContextExtraArguments struct {
	AdaptersEnabled        bool
	AdapterStrengthEnabled bool
	PerformanceMode        string // vLLM --performance-mode; defaults to "balanced"
}

func (p *PresetParam) GetInferenceCommand(rc RuntimeContext) []string {
	switch rc.RuntimeName {
	case RuntimeNameHuggingfaceTransformers:
		return p.buildHuggingfaceInferenceCommand()
	case RuntimeNameVLLM:
		return p.buildVLLMInferenceCommand(rc)
	default:
		return nil
	}
}

func (p *PresetParam) buildHuggingfaceInferenceCommand() []string {
	if p.Transformers.ModelName != "" {
		p.Transformers.ModelRunParams["served_model_name"] = p.Transformers.ModelName
	}
	if p.DownloadAtRuntime {
		repoId, revision, _ := utils.ParseHuggingFaceModelVersion(p.Version)
		p.Transformers.ModelRunParams["pretrained_model_name_or_path"] = repoId
		if revision != "" {
			p.Transformers.ModelRunParams["revision"] = revision
		}
		p.Transformers.ModelRunParams["allow_remote_files"] = ""
	}
	torchCommand := utils.BuildCmdStr(
		p.Transformers.BaseCommand,
		p.Transformers.AccelerateParams,
	)
	modelCommand := utils.BuildCmdStr(
		p.Transformers.InferenceMainFile,
		p.Transformers.ModelRunParams,
	)
	return utils.ShellCmd(torchCommand + " " + modelCommand)
}

func (p *PresetParam) buildVLLMInferenceCommand(rc RuntimeContext) []string {
	// If the Workspace was created by an InferenceSet, expose the InferenceSet
	// name as the served model name so all replicas behind the InferenceSet
	// share a single, stable model identifier in the OpenAI-compatible API.
	// Standalone Workspaces keep the model's default served name.
	if isName, ok := rc.WorkspaceMetadata.Labels[consts.WorkspaceCreatedByInferenceSetLabel]; ok && isName != "" {
		p.VLLM.ModelRunParams["served-model-name"] = isName
	} else if p.VLLM.ModelName != "" {
		p.VLLM.ModelRunParams["served-model-name"] = p.VLLM.ModelName
	}
	if rc.MaxModelLen > 0 {
		p.VLLM.ModelRunParams["max-model-len"] = strconv.Itoa(rc.MaxModelLen)
	}
	p.VLLM.ModelRunParams["gpu-memory-utilization"] = "0.84"

	// Dynamically determine dtype based on GPU compute capability.
	// bfloat16 requires CUDA compute capability >= 8.0 (Ampere+).
	// Fall back to float16 on older GPUs.
	if rc.GPUConfig != nil && !rc.GPUConfig.SupportsBFloat16() {
		p.VLLM.ModelRunParams["dtype"] = "float16"
	}

	if !p.VLLM.DisallowLoRA && rc.AdaptersEnabled {
		p.VLLM.ModelRunParams["enable-lora"] = ""
	}
	if p.DownloadAtRuntime {
		repoId, revision, _ := utils.ParseHuggingFaceModelVersion(p.Version)
		p.VLLM.ModelRunParams["model"] = repoId
		if revision != "" {
			p.VLLM.ModelRunParams["code-revision"] = revision
		}
		p.VLLM.ModelRunParams["download-dir"] = utils.DefaultWeightsVolumePath
	}
	if rc.ConfigVolume != nil {
		p.VLLM.ModelRunParams["kaito-config-file"] = path.Join(rc.ConfigVolume.MountPath, ConfigfileNameVLLM)
	}
	if rc.PerformanceMode != "" && rc.PerformanceMode != "balanced" {
		p.VLLM.ModelRunParams["performance-mode"] = rc.PerformanceMode
	}

	// Hybrid Mamba/Attention models (e.g., NemotronH) require the hybrid KV cache
	// manager in vLLM, which is incompatible with LMCache KV cache CPU offloading.
	// Disable offloading for these architectures to prevent startup crashes.
	if p.isVLLMHybridKVCacheManagerRequired() {
		p.VLLM.ModelRunParams["kaito-kv-cache-cpu-memory-utilization"] = "0"
	}

	// Parallelism strategy follows a 3-tier hierarchy (see configureParallelism):
	//  1. Data Parallelism (DP)   – model fits on a single GPU
	//  2. Tensor Parallelism (TP) – model fits on a single node (multiple GPUs)
	//  3. Pipeline Parallelism (PP) + TP – model requires multiple nodes
	p.configureParallelism(rc)

	// Single-node path: no Ray cluster needed.
	if !rc.DistributedInference || rc.NumNodes == 1 {
		modelCommand := utils.BuildCmdStr(p.VLLM.BaseCommand, p.VLLM.ModelRunParams)
		return utils.ShellCmd(modelCommand)
	}

	// Multi-node path: set up a Ray cluster for cross-node parallelism.
	return p.buildMultiNodeRayCommand(rc)
}

// configureParallelism sets the vLLM parallelism parameters according to a
// 3-tier strategy based on where the model can be placed:
//
//  1. Single-GPU (DP): If the model file size is less than 50% of a single GPU's
//     memory, each GPU can serve the model independently. We use data parallelism
//     to run replicas across GPUs for maximum throughput.
//
//  2. Single-node (TP): If the model is too large for one GPU but fits within the
//     combined memory of all GPUs on a single node, we shard the model across GPUs
//     using tensor parallelism.
//
//  3. Multi-node (PP + TP): If the model exceeds a single node's capacity, we use
//     pipeline parallelism across nodes, with tensor parallelism within each node.
func (p *PresetParam) configureParallelism(rc RuntimeContext) {
	if p.DisableTensorParallelism {
		return
	}

	multiNode := rc.DistributedInference && rc.NumNodes > 1

	// Tier 1: Model fits on a single GPU → Data Parallelism.
	// Use DP only on a single node; multi-node DP is not supported.
	if !multiNode && p.modelFitsOnSingleGPU(rc) {
		p.VLLM.ModelRunParams["data-parallel-size"] = strconv.Itoa(rc.SKUNumGPUs)
		p.VLLM.ModelRunParams["tensor-parallel-size"] = "1"
		// In this branch, data-parallel-size is guaranteed to be > 1; disable kv cache CPU offloading
		// due to conflicts between data parallelism and CPU offloading.
		p.VLLM.ModelRunParams["kaito-kv-cache-cpu-memory-utilization"] = "0"
		return
	}

	// Tier 2: Model fits on a single node → Tensor Parallelism.
	// TP is set to the number of GPUs on the node.
	p.VLLM.ModelRunParams["tensor-parallel-size"] = strconv.Itoa(rc.SKUNumGPUs)

	// Tier 3: Model requires multiple nodes → Pipeline Parallelism + TP.
	if multiNode {
		// Disable kv cache CPU offloading when pipeline parallelism is enabled.
		// TODO: LMCache doesn't support cross-node PP in CPU offload mode.
		p.VLLM.ModelRunParams["kaito-kv-cache-cpu-memory-utilization"] = "0"

		// PP is set to the number of nodes.
		p.VLLM.ModelRunParams["pipeline-parallel-size"] = strconv.Itoa(rc.NumNodes)

		// Since vllm 0.12.0, we need to set the distributed-executor-backend explicitly.
		p.VLLM.ModelRunParams["distributed-executor-backend"] = "ray"
	}
}

// buildMultiNodeRayCommand constructs the shell command for multi-node inference
// using a Ray cluster. Pod index 0 is the leader; all other pods are workers.
func (p *PresetParam) buildMultiNodeRayCommand(rc RuntimeContext) []string {
	if p.VLLM.RayLeaderParams == nil {
		p.VLLM.RayLeaderParams = make(map[string]string)
	}
	p.VLLM.RayLeaderParams["ray_cluster_size"] = strconv.Itoa(rc.NumNodes)
	p.VLLM.RayLeaderParams["ray_port"] = strconv.Itoa(PortRayCluster)

	if p.VLLM.RayWorkerParams == nil {
		p.VLLM.RayWorkerParams = make(map[string]string)
	}
	p.VLLM.RayWorkerParams["ray_address"] = utils.GetRayLeaderHost(rc.WorkspaceMetadata)
	p.VLLM.RayWorkerParams["ray_port"] = strconv.Itoa(PortRayCluster)

	rayLeaderCommand := utils.BuildCmdStr(p.VLLM.RayLeaderBaseCommand, p.VLLM.RayLeaderParams)
	modelRunCommand := utils.BuildCmdStr(p.VLLM.BaseCommand, p.VLLM.ModelRunParams)
	result := utils.BuildIfElseCmdStr(
		`[ "${POD_INDEX}" = "0" ]`,                                      // leader if pod index is 0, otherwise worker
		strings.Join([]string{rayLeaderCommand, modelRunCommand}, "; "), // leader: start ray head + model
		map[string]string{},
		p.VLLM.RayWorkerBaseCommand, // worker: join the cluster
		p.VLLM.RayWorkerParams,
	)

	return utils.ShellCmd(result)
}

// getModelFileSize returns the model file size as a resource.Quantity.
// It tries TotalSafeTensorFileSize first (preset models), then ModelFileSize (best-effort models).
func (p *PresetParam) getModelFileSize() *resource.Quantity {
	if p.TotalSafeTensorFileSize != "" {
		q, err := resource.ParseQuantity(p.TotalSafeTensorFileSize)
		if err == nil {
			return &q
		}
	}
	if p.ModelFileSize != "" {
		q, err := resource.ParseQuantity(p.ModelFileSize)
		if err == nil {
			return &q
		}
	}
	return nil
}

// isVLLMHybridKVCacheManagerRequired returns true if the model uses a hybrid
// architecture (e.g., Mamba/Attention) that requires vLLM's hybrid KV cache manager
// (https://docs.vllm.ai/en/latest/design/hybrid_kv_cache_manager/)
func (p *PresetParam) isVLLMHybridKVCacheManagerRequired() bool {
	for _, arch := range p.Architectures {
		switch arch {
		case "NemotronHForCausalLM", "NemotronH_Nano_VL_V2", "NemotronHMTPModel", "NemotronHPuzzleForCausalLM",
			"Gemma4ForCausalLM", "Gemma4ForConditionalGeneration":
			return true
		}
	}
	return false
}

// modelFitsOnSingleGPU returns true when the model file size is smaller than
// 50% of a single GPU's memory, meaning the entire model can be loaded onto
// one GPU with headroom to spare.
func (p *PresetParam) modelFitsOnSingleGPU(rc RuntimeContext) bool {
	if rc.GPUConfig == nil || rc.SKUNumGPUs <= 1 {
		return false
	}
	modelSize := p.getModelFileSize()
	if modelSize == nil {
		return false
	}
	// Single GPU memory = total GPU memory / number of GPUs.
	// Condition: modelSize < 0.5 * singleGPUMem
	// Rearranged to avoid division: modelSize * numGPUs * 2 < totalGPUMem.
	return modelSize.Value()*int64(rc.SKUNumGPUs)*2 < rc.GPUConfig.GPUMem.Value()
}

func (p *PresetParam) Validate(rc RuntimeContext) error {
	var errs []string
	switch rc.RuntimeName {
	case RuntimeNameHuggingfaceTransformers:
		if p.Transformers.BaseCommand == "" {
			errs = append(errs, fmt.Sprintf("model %s does not support inference with Huggingface Transformers runtime", p.Metadata.Name))
		}
	case RuntimeNameVLLM:
		if rc.AdaptersEnabled && p.VLLM.DisallowLoRA {
			errs = append(errs, fmt.Sprintf("vLLM does not support LoRA adapters for this model: %s", p.VLLM.ModelName))
		}
		if rc.AdapterStrengthEnabled {
			errs = append(errs, "vLLM does not support adapter strength")
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// Only support Huggingface for now
func (p *PresetParam) GetTuningCommand(rc RuntimeContext) []string {
	if p.Transformers.AccelerateParams == nil {
		p.Transformers.AccelerateParams = make(map[string]string)
	}

	p.Transformers.AccelerateParams["num_processes"] = strconv.Itoa(rc.SKUNumGPUs)
	torchCommand := utils.BuildCmdStr(p.Transformers.BaseCommand, p.Transformers.AccelerateParams)
	modelCommand := utils.BuildCmdStr(DefaultTuningMainFile, p.Transformers.ModelRunParams)
	return utils.ShellCmd(torchCommand + " " + modelCommand)
}
