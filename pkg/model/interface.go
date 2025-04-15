// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package model

import (
	"fmt"
	"maps"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
)

type Model interface {
	GetInferenceParameters() *PresetParam
	GetTuningParameters() *PresetParam
	SupportDistributedInference() bool //If true, the model workload will be a StatefulSet, using the torch elastic runtime framework.
	SupportTuning() bool
}

// RuntimeName is LLM runtime name.
type RuntimeName string

const (
	RuntimeNameHuggingfaceTransformers RuntimeName = "transformers"
	RuntimeNameVLLM                    RuntimeName = "vllm"

	ConfigfileNameVLLM    = "inference_config.yaml"
	DefaultMemoryUtilVLLM = 0.9 // Default gpu memory utilization for VLLM runtime
	UpperMemoryUtilVLLM   = 0.95
)

var (
	// vLLM will do kvcache pre-allocation,
	// We need to reserve enough memory for other ephemeral operations to avoid OOM.
	// This is an empirical value.
	ReservedNonKVCacheMemory = resource.MustParse("1.5Gi")
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
	BaseCommand        string            // The initial command (e.g., 'torchrun', 'accelerate launch') used in the command line.
	TorchRunParams     map[string]string // Parameters for configuring the torchrun command.
	TorchRunRdzvParams map[string]string // Optional rendezvous parameters for distributed training/inference using torchrun (elastic).
	InferenceMainFile  string            // The main file for inference.
	ModelRunParams     map[string]string // Parameters for running the model training/inference.
}

type VLLMParam struct {
	BaseCommand string
	// The model name used in the openai serving API.
	// see https://platform.openai.com/docs/api-reference/chat/create#chat-create-model.
	ModelName string
	// Parameters for distributed inference.
	DistributionParams map[string]string
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
	out.TorchRunParams = maps.Clone(h.TorchRunParams)
	out.TorchRunRdzvParams = maps.Clone(h.TorchRunRdzvParams)
	out.ModelRunParams = maps.Clone(h.ModelRunParams)
	return out
}

func (v *VLLMParam) DeepCopy() VLLMParam {
	if v == nil {
		return VLLMParam{}
	}
	out := *v
	out.DistributionParams = maps.Clone(v.DistributionParams)
	out.ModelRunParams = maps.Clone(v.ModelRunParams)
	return out
}

// RuntimeContext defines the runtime context for a model.
type RuntimeContext struct {
	RuntimeName  RuntimeName
	GPUConfig    *sku.GPUConfig
	ConfigVolume *corev1.VolumeMount
	SKUNumGPUs   int
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
	torchCommand := utils.BuildCmdStr(
		p.Transformers.BaseCommand,
		p.Transformers.TorchRunParams,
		p.Transformers.TorchRunRdzvParams,
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
	if !p.DisableTensorParallelism {
		p.VLLM.ModelRunParams["tensor-parallel-size"] = strconv.Itoa(rc.SKUNumGPUs)
	}
	if !p.VLLM.DisallowLoRA && rc.AdaptersEnabled {
		p.VLLM.ModelRunParams["enable-lora"] = ""
	}
	gpuMemUtil := getGPUMemoryUtilForVLLM(rc.GPUConfig)
	p.VLLM.ModelRunParams["gpu-memory-utilization"] = strconv.FormatFloat(gpuMemUtil, 'f', 2, 64)
	if rc.ConfigVolume != nil {
		p.VLLM.ModelRunParams["kaito-config-file"] = path.Join(rc.ConfigVolume.MountPath, ConfigfileNameVLLM)
	}
	modelCommand := utils.BuildCmdStr(p.VLLM.BaseCommand, p.VLLM.ModelRunParams)
	return utils.ShellCmd(modelCommand)
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

func getGPUMemoryUtilForVLLM(gpuConfig *sku.GPUConfig) float64 {
	if gpuConfig == nil || gpuConfig.GPUMemGB <= 0 || gpuConfig.GPUCount <= 0 {
		return DefaultMemoryUtilVLLM
	}

	gpuMem := resource.MustParse(fmt.Sprintf("%dGi", gpuConfig.GPUMemGB))
	gpuMemPerGPU := float64(gpuMem.Value()) / float64(gpuConfig.GPUCount)

	if float64(ReservedNonKVCacheMemory.Value()) >= gpuMemPerGPU {
		// looks impossible, just prevent this case
		return DefaultMemoryUtilVLLM
	}

	util := math.Floor((1.0-float64(ReservedNonKVCacheMemory.Value())/gpuMemPerGPU)*100) / 100
	return math.Min(util, UpperMemoryUtilVLLM)
}
