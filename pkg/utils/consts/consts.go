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

	// Node provisioner types
	NodeProvisionerAzureGPU  = "azure-gpu-provisioner"
	NodeProvisionerKarpenter = "karpenter"
	NodeProvisionerBYO       = "byo"
)

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
	LabelProvisionerName      = "karpenter.sh/provisioner-name"

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
	EPPImageTag  = "v0.7.1"

	// ConditionReady is the condition type for a ready condition.
	ConditionReady = "Ready"

	WorkspaceCreatedByInferenceSetLabel = "inferenceset.kaito.sh/created-by"

	NodeImageFamilyUbuntu     = "ubuntu"
	NodeImageFamilyAzureLinux = "azurelinux"
	SpotInstanceKey           = "kubernetes.azure.com/scalesetpriority"
	SpotInstanceValue         = "spot"

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
