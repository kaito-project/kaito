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

package consts

import (
	"strings"
	"time"
)

const (
	// WorkspaceFinalizer is used to make sure that workspace controller handles garbage collection.
	WorkspaceFinalizer = "workspace.finalizer.kaito.sh"
	// InferenceSetFinalizer is used to make sure that inferenceset controller handles garbage collection.
	InferenceSetFinalizer = "inferenceset.finalizer.kaito.sh"
	// RAGEngineFinalizer is used to make sure that ragengine controller handles garbage collection.
	RAGEngineFinalizer            = "ragengine.finalizer.kaito.sh"
	DefaultReleaseNamespaceEnvVar = "RELEASE_NAMESPACE"
	AzureCloudName                = "azure"
	AWSCloudName                  = "aws"
	ArcCloudName                  = "arc"
	GPUString                     = "gpu"
	SKUString                     = "sku"
	MaxRevisionHistoryLimit       = 10
	GiBToBytes                    = 1024 * 1024 * 1024 // Conversion factor from GiB to bytes
	MiBToBytes                    = 1024 * 1024        // Conversion factor from MiB to bytes
	NvidiaGPU                     = "nvidia.com/gpu"
	NvidiaGPUProduct              = "nvidia.com/gpu.product"
	NvidiaGPUCount                = "nvidia.com/gpu.count"
	NvidiaGPUMemory               = "nvidia.com/gpu.memory"
	NvidiaCUDAComputeCapMajor     = "nvidia.com/cuda.compute.major"
	NvidiaCUDAComputeCapMinor     = "nvidia.com/cuda.compute.minor"

	// Feature flags
	FeatureFlagVLLM                         = "vLLM"
	FeatureFlagDisableNodeAutoProvisioning  = "disableNodeAutoProvisioning"
	FeatureFlagGatewayAPIInferenceExtension = "gatewayAPIInferenceExtension"
	FeatureFlagEnableInferenceSetController = "enableInferenceSetController"

	FeatureFlagEnableMultiRoleInferenceController = "enableMultiRoleInferenceController"
	FeatureFlagModelMirror                        = "ModelMirror"
	FeatureFlagModelStreaming                     = "ModelStreaming"
	FeatureFlagEnableBaseImageAutoUpgrade         = "enableBaseImageAutoUpgrade"

	// Node provisioner types
	NodeProvisionerAzureGPU  = "azure-gpu-provisioner"
	NodeProvisionerKarpenter = "karpenter"
	NodeProvisionerBYO       = "byo"

	// CSI driver names for model streaming (workspace controller + webhook scope).
	CSIDriverNameAzureBlob = "blob.csi.azure.com"
)

// CSIDriverNameForCloud returns the expected CSI driver name for the given cloud provider.
// Returns "" for unsupported providers.
func CSIDriverNameForCloud(cloud string) string {
	switch cloud {
	case AzureCloudName:
		return CSIDriverNameAzureBlob
	default:
		return ""
	}
}

// ActiveNodeProvisioner holds the resolved provisioner type at runtime.
// Set once during startup in main.go; read by inference scheduling code
// to decide whether karpenter-specific nodeSelector/tolerations are needed.
var ActiveNodeProvisioner string

// IsKarpenterProvisioner returns true if the active node provisioner is karpenter.
func IsKarpenterProvisioner() bool {
	return ActiveNodeProvisioner == NodeProvisionerKarpenter
}

const (
	// Nodeclaim related consts
	KaitoNodePoolName             = "kaito"
	LabelNodePool                 = "karpenter.sh/nodepool"
	ErrorInstanceTypesUnavailable = "all requested instance types were unavailable during launch"
	NodeClassName                 = "default"

	// Karpenter provisioner related consts
	KarpenterLabelManagedBy    = "karpenter.kaito.sh/managed-by"
	KarpenterManagedByValue    = "kaito"
	AKSNodeClassUbuntuName     = "image-family-ubuntu"
	AKSNodeClassAzureLinuxName = "image-family-azure-linux"
	AKSNodeClassOSDiskSizeGB   = 300

	// machine related consts
	ProvisionerName           = "default"
	LabelGPUProvisionerCustom = "kaito.sh/machine-type"

	// azure gpu sku prefix
	GpuSkuPrefix = "Standard_N"

	NodePluginInstallTimeout = 60 * time.Second

	// PortInferenceServer is the default port for the inference server.
	PortInferenceServer = int32(5000)

	// InferencePoolChartURL is the OCI registry URL for the Gateway API Inference Extension inferencepool chart.
	InferencePoolChartURL = "oci://registry.k8s.io/gateway-api-inference-extension/charts/inferencepool"

	// InferencePoolChartVersion is the tag/version of the inferencepool chart to deploy.
	// MUST KEEP IN SYNC with the version in go.mod.
	InferencePoolChartVersion = "v1.3.1"

	// EPP (Endpoint Picker) image configuration.
	// The InferencePool chart composes the image as: {hub}/{name}:{tag}
	// Using llm-d inference scheduler which consolidates the GWIE EPP implementation
	// with advanced scheduling plugins (KV cache-aware routing, P/D disaggregation, etc.)
	// See: https://github.com/llm-d/llm-d-inference-scheduler
	EPPImageHub  = "mcr.microsoft.com/oss/v2/llm-d"
	EPPImageName = "llm-d-inference-scheduler"
	EPPImageTag  = "v0.8.0"

	// TokenizerSidecar runs a GPU-less vLLM render process for tokenization.
	// It exposes /v1/completions/render and /v1/chat/completions/render on port 8100.
	// Used by the EPP token-producer plugin for prefix-cache-aware routing when enabled.
	// Currently disabled by default; only needed if the EPP plugin pipeline requires
	// a token producer (e.g., precise-prefix-cache-scorer instead of approx-prefix-cache-producer).
	TokenizerSidecarImage = "mcr.microsoft.com/oss/v2/vllm/vllm-openai-cpu:v0.21.0"
	TokenizerSidecarPort  = 8100

	// Routing sidecar for P/D disaggregation on decode workspaces.
	// The sidecar listens on port 5000 (PortInferenceServer) so the Service
	// can target port 5000 uniformly across prefill and decode pods.
	// vLLM on decode pods is moved to port 5001 (PortDecodeVLLM).
	// See: https://github.com/llm-d/llm-d-routing-sidecar
	RoutingSidecarImage = "mcr.microsoft.com/oss/v2/llm-d/llm-d-routing-sidecar"
	RoutingSidecarTag   = "v0.8.0"

	// PortDecodeVLLM is the port vLLM listens on in decode pods.
	// The routing sidecar occupies port 5000 (PortInferenceServer), so vLLM
	// is moved to 5001. The sidecar forwards traffic to this port.
	PortDecodeVLLM = int32(5001)

	// InferenceRoleEnvName is the environment variable name used to pass the
	// inference role (prefill/decode) to the model container in P/D disaggregated serving.
	InferenceRoleEnvName = "KAITO_INFERENCE_ROLE"

	// VLLMUseFlashInferSamplerEnvName toggles vLLM's FlashInfer-based sampler.
	// KAITO does not support FlashInfer, so it is set to "0" to keep vLLM on the
	// Torch-native sampling path and avoid runtime JIT kernel compilation, which
	// requires a CUDA toolchain (nvcc) that the base image does not ship.
	VLLMUseFlashInferSamplerEnvName = "VLLM_USE_FLASHINFER_SAMPLER"

	// VLLMUseDeepGEMMEnvName toggles vLLM's DeepGEMM FP8 kernels. vLLM 0.22.1
	// defaults this on and reports DeepGEMM as available (it finds the vendored
	// wrapper module), but the native FP8 GEMM backend is not present in the base
	// image, so the FP8 warmup hard-fails with "DeepGEMM backend is not available".
	// Set to "0" to keep FP8 models on their non-DeepGEMM kernel path.
	VLLMUseDeepGEMMEnvName = "VLLM_USE_DEEP_GEMM"

	// The VLLMUseFlashInferMoe*EnvName variables toggle vLLM's per-precision
	// FlashInfer MoE backends. For MoE models vLLM auto-selects a FlashInfer
	// (TRTLLM/CUTLASS) expert kernel, which JIT-compiles at runtime via nvcc — a
	// CUDA toolchain the base image does not ship — crashing the engine at startup.
	// Setting each explicitly to "0" removes the FlashInfer backends from the
	// candidate list so vLLM falls back to the Triton MoE kernel, which needs no
	// nvcc JIT (KAITO does not support FlashInfer). One variable exists per MoE
	// weight/activation precision, so all must be disabled to cover every model.
	VLLMUseFlashInferMoeFP16EnvName              = "VLLM_USE_FLASHINFER_MOE_FP16"
	VLLMUseFlashInferMoeFP8EnvName               = "VLLM_USE_FLASHINFER_MOE_FP8"
	VLLMUseFlashInferMoeFP4EnvName               = "VLLM_USE_FLASHINFER_MOE_FP4"
	VLLMUseFlashInferMoeMXFP4BF16EnvName         = "VLLM_USE_FLASHINFER_MOE_MXFP4_BF16"
	VLLMUseFlashInferMoeMXFP4MXFP8EnvName        = "VLLM_USE_FLASHINFER_MOE_MXFP4_MXFP8"
	VLLMUseFlashInferMoeMXFP4MXFP8CutlassEnvName = "VLLM_USE_FLASHINFER_MOE_MXFP4_MXFP8_CUTLASS"

	// ConditionReady is the condition type for a ready condition.
	ConditionReady = "Ready"

	WorkspaceCreatedByInferenceSetLabel = "inferenceset.kaito.sh/created-by"

	NodeImageFamilyUbuntu     = "ubuntu"
	NodeImageFamilyAzureLinux = "azurelinux"
	SpotInstanceKey           = "kubernetes.azure.com/scalesetpriority"
	SpotInstanceValue         = "spot"

	// Azure karpenter-provider-azure labels.
	AzurePlacementScopeLabel = "karpenter.azure.com/placement-scope"
	AzurePlacementRegional   = "regional"

	// Karpenter NodePool management labels and values.
	KarpenterWorkspaceNameKey         = "karpenter.kaito.sh/workspace-name"
	KarpenterWorkspaceNamespaceKey    = "karpenter.kaito.sh/workspace-namespace"
	KarpenterInferenceSetKey          = "karpenter.kaito.sh/inferenceset"
	KarpenterInferenceSetNamespaceKey = "karpenter.kaito.sh/inferenceset-namespace"
)

var (
	LocalNVMeStorageClass = "kaito-local-nvme-disk"
)

func NormalizeSupportedNodeImageFamily(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case NodeImageFamilyUbuntu, NodeImageFamilyAzureLinux:
		return normalized, true
	default:
		return "", false
	}
}
