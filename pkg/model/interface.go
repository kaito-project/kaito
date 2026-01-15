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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
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

	// DType specifies the data type used by the model (e.g., "bfloat16", "float16", "float32").
	// This field is part of the shared Metadata struct and is primarily populated in YAML for best effort supported models.
	// +optional
	DType string `yaml:"dtype,omitempty"`

	// ModelFileSize is the size of the model file, example: 14Gi.
	// This field is part of the shared Metadata struct and is primarily populated in YAML for best effort supported models.
	ModelFileSize string `yaml:"modelFileSize,omitempty"`

	// DiskStorageRequirement is the disk storage requirement for the model, example: 90Gi.
	// This field is part of the shared Metadata struct and is primarily populated in YAML for best effort supported models.
	DiskStorageRequirement string `yaml:"diskStorageRequirement,omitempty"`

	// BytesPerToken is the number of bytes used to represent each token in the model.
	// This field is part of the shared Metadata struct and is primarily populated in YAML for best effort supported models.
	BytesPerToken int `yaml:"bytesPerToken,omitempty"`

	// ModelTokenLimit is the maximum number of tokens (context window) supported by the model.
	// This field is part of the shared Metadata struct and is primarily populated in YAML for best effort supported models.
	ModelTokenLimit int `yaml:"modelTokenLimit,omitempty"`

	// ToolCallParser specifies the parser used for tool calls within the model.
	// This field is part of the shared Metadata struct and is primarily populated in YAML for best effort supported models.
	// +optional
	ToolCallParser string `yaml:"toolCallParser,omitempty"`

	// ReasoningParser specifies the parser used for reasoning within the model.
	// This field is part of the shared Metadata struct and is primarily populated in YAML for best effort supported models.
	// +optional
	ReasoningParser string `yaml:"reasoningParser,omitempty"`

	// ChatTemplate is the chat template file name used for chat models.
	// This field is part of the shared Metadata struct and is primarily populated in YAML for best effort supported models.
	// +optional
	ChatTemplate string `yaml:"chatTemplate,omitempty"`

	// AllowRemoteFiles indicates whether the model allows loading remote files.
	// This field is part of the shared Metadata struct and is primarily populated in YAML for best effort supported models.
	// +optional
	AllowRemoteFiles bool `yaml:"allowRemoteFiles,omitempty"`
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
	if p.DownloadAtRuntime {
		repoId, revision, _ := utils.ParseHuggingFaceModelVersion(p.Version)
		p.Transformers.ModelRunParams["pretrained_model_name_or_path"] = repoId
		if revision != "" {
			p.Transformers.ModelRunParams["revision"] = revision
		}
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
	if p.VLLM.ModelName != "" {
		p.VLLM.ModelRunParams["served-model-name"] = p.VLLM.ModelName
	}
	if rc.MaxModelLen > 0 {
		p.VLLM.ModelRunParams["max-model-len"] = strconv.Itoa(rc.MaxModelLen)
	}
	p.VLLM.ModelRunParams["gpu-memory-utilization"] = "0.84"

	if !p.DisableTensorParallelism {
		// Tensor Parallelism (TP) is set to the number of GPUs on a given node per vLLM guidance:
		// https://docs.vllm.ai/en/latest/serving/distributed_serving.html.
		p.VLLM.ModelRunParams["tensor-parallel-size"] = strconv.Itoa(rc.SKUNumGPUs)
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

	// If user wants to deploy a model that supports distributed inference, but
	// there is only one node, we don't need to setup a multi-node Ray cluster.
	if !rc.DistributedInference || rc.NumNodes == 1 {
		modelCommand := utils.BuildCmdStr(p.VLLM.BaseCommand, p.VLLM.ModelRunParams)
		return utils.ShellCmd(modelCommand)
	}

	// Pipeline Parallelism (PP) is set to the number of nodes for multi-node inference per vLLM guidance:
	// https://docs.vllm.ai/en/latest/serving/distributed_serving.html.
	p.VLLM.ModelRunParams["pipeline-parallel-size"] = strconv.Itoa(rc.NumNodes)

	// Since vllm 0.12.0, we need to set the distributed-executor-backend explicitly
	p.VLLM.ModelRunParams["distributed-executor-backend"] = "ray"

	// We need to setup multi-node Ray cluster and assume pod index 0 is the leader of the cluster.
	// - leader: start as ray leader along with the model run command
	// - worker: start as ray worker - don't need to provide the model run command
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
		`[ "${POD_INDEX}" = "0" ]`,                                      // initiate as ray leader if the pod index is 0, otherwise initiate as ray worker
		strings.Join([]string{rayLeaderCommand, modelRunCommand}, "; "), // command if true, concatenate the ray leader command and model run command
		map[string]string{},                                             // no parameters needed since the command is already built above
		p.VLLM.RayWorkerBaseCommand,                                     // command if false
		p.VLLM.RayWorkerParams,                                          // parameters for the false command
	)

	return utils.ShellCmd(result)
}

func (p *PresetParam) Validate(rc RuntimeContext) error {
	var errs []string
	switch rc.RuntimeName {
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
