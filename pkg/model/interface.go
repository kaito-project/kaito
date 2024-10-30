// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package model

import (
	"time"

	"github.com/azure/kaito/pkg/utils"
)

type Model interface {
	GetInferenceParameters() *PresetParam
	GetTuningParameters() *PresetParam
	SupportDistributedInference() bool //If true, the model workload will be a StatefulSet, using the torch elastic runtime framework.
	SupportTuning() bool
}

// BackendName is LLM runtime name.
type BackendName string

const (
	BackendNameHuggingfaceTransformers BackendName = "transformers"
	BackendNameVLLM                    BackendName = "vllm"

	InferenceFileHuggingface = "/workspace/tfs/inference_api.py"
	InferenceFileVLLM        = "/workspace/vllm/inference_api.py"
)

// PresetParam defines the preset inference parameters for a model.
type PresetParam struct {
	Tag             string // The model image tag
	ModelFamilyName string // The name of the model family.
	ImageAccessMode string // Defines where the Image is Public or Private.

	DiskStorageRequirement        string         // Disk storage requirements for the model.
	GPUCountRequirement           string         // Number of GPUs required for the Preset. Used for inference.
	TotalGPUMemoryRequirement     string         // Total GPU memory required for the Preset. Used for inference.
	PerGPUMemoryRequirement       string         // GPU memory required per GPU. Used for inference.
	TuningPerGPUMemoryRequirement map[string]int // Min GPU memory per tuning method (batch size 1). Used for tuning.
	WorldSize                     int            // Defines the number of processes required for distributed inference.

	BackendParam

	// ReadinessTimeout defines the maximum duration for creating the workload.
	// This timeout accommodates the size of the image, ensuring pull completion
	// even under slower network conditions or unforeseen delays.
	ReadinessTimeout time.Duration
}

// BackendParam defines the llm backend parameters.
type BackendParam struct {
	Transformers HuggingfaceTransformersParam
	VLLM         VLLMParam
}

type HuggingfaceTransformersParam struct {
	BaseCommand        string            // The initial command (e.g., 'torchrun', 'accelerate launch') used in the command line.
	TorchRunParams     map[string]string // Parameters for configuring the torchrun command.
	TorchRunRdzvParams map[string]string // Optional rendezvous parameters for distributed training/inference using torchrun (elastic).
	ModelRunParams     map[string]string // Parameters for running the model training/inference.
}

type VLLMParam struct {
	BaseCommand        string            // The initial command (e.g., 'torchrun', 'accelerate launch') used in the command line.
	DistributionParams map[string]string // Parameters for distributed inference.
	ModelRunParams     map[string]string // Parameters for running the model training/inference.
}

func (p *PresetParam) DeepCopy() *PresetParam {
	if p == nil {
		return nil
	}
	out := new(PresetParam)
	*out = *p
	out.BackendParam = p.BackendParam.DeepCopy()
	out.TuningPerGPUMemoryRequirement = make(map[string]int, len(p.TuningPerGPUMemoryRequirement))
	for k, v := range p.TuningPerGPUMemoryRequirement {
		out.TuningPerGPUMemoryRequirement[k] = v
	}
	return out
}

func (rp *BackendParam) DeepCopy() BackendParam {
	if rp == nil {
		return BackendParam{}
	}
	out := BackendParam{}
	out.Transformers = rp.Transformers.DeepCopy()
	out.VLLM = rp.VLLM.DeepCopy()
	return out
}

func (h *HuggingfaceTransformersParam) DeepCopy() HuggingfaceTransformersParam {
	if h == nil {
		return HuggingfaceTransformersParam{}
	}
	out := HuggingfaceTransformersParam{}
	out.BaseCommand = h.BaseCommand
	out.TorchRunParams = make(map[string]string, len(h.TorchRunParams))
	for k, v := range h.TorchRunParams {
		out.TorchRunParams[k] = v
	}
	out.TorchRunRdzvParams = make(map[string]string, len(h.TorchRunRdzvParams))
	for k, v := range h.TorchRunRdzvParams {
		out.TorchRunRdzvParams[k] = v
	}
	out.ModelRunParams = make(map[string]string, len(h.ModelRunParams))
	for k, v := range h.ModelRunParams {
		out.ModelRunParams[k] = v
	}
	return out
}

func (v *VLLMParam) DeepCopy() VLLMParam {
	if v == nil {
		return VLLMParam{}
	}
	out := VLLMParam{}
	out.BaseCommand = v.BaseCommand
	out.DistributionParams = make(map[string]string, len(v.DistributionParams))
	for k, v := range v.DistributionParams {
		out.DistributionParams[k] = v
	}
	out.ModelRunParams = make(map[string]string, len(v.ModelRunParams))
	for k, v := range v.ModelRunParams {
		out.ModelRunParams[k] = v
	}
	return out
}

// builds the container command:
// eg. torchrun <TORCH_PARAMS> <OPTIONAL_RDZV_PARAMS> baseCommand <MODEL_PARAMS>
func (p *PresetParam) GetInferenceCommand(backend BackendName) []string {
	switch backend {
	case BackendNameHuggingfaceTransformers:
		torchCommand := utils.BuildCmdStr(p.Transformers.BaseCommand, p.Transformers.TorchRunParams, p.Transformers.TorchRunRdzvParams)
		modelCommand := utils.BuildCmdStr(InferenceFileHuggingface, p.Transformers.ModelRunParams)
		return utils.ShellCmd(torchCommand + " " + modelCommand)
	case BackendNameVLLM:
		modelCommand := utils.BuildCmdStr(InferenceFileVLLM, p.VLLM.ModelRunParams)
		return utils.ShellCmd(p.VLLM.BaseCommand + " " + modelCommand)
	default:
		return nil
	}
}
