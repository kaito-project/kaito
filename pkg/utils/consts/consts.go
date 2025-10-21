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

import "time"

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

	// Feature flags
	FeatureFlagVLLM                         = "vLLM"
	FeatureFlagEnsureNodeClass              = "ensureNodeClass"
	FeatureFlagDisableNodeAutoProvisioning  = "disableNodeAutoProvisioning"
	FeatureFlagGatewayAPIInferenceExtension = "gatewayAPIInferenceExtension"
	FeatureFlagEnableInferenceSetController = "enableInferenceSetController"

	// Nodeclaim related consts
	KaitoNodePoolName             = "kaito"
	LabelNodePool                 = "karpenter.sh/nodepool"
	ErrorInstanceTypesUnavailable = "all requested instance types were unavailable during launch"
	NodeClassName                 = "default"

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
	InferencePoolChartVersion = "v1.0.1"

	// GatewayAPIInferenceExtensionImageRepository is the image repository for the Gateway API Inference Extension components.
	GatewayAPIInferenceExtensionImageRepository = "mcr.microsoft.com/oss/v2/gateway-api-inference-extension"

	// ConditionReady is the condition type for a ready condition.
	ConditionReady = "Ready"

	WorkspaceCreatedByInferenceSetLabel = "inferenceset.kaito.sh/created-by"
	InferenceSetMemberLabel             = "inferenceset.kaito.sh/member"
)

var (
	LocalNVMeStorageClass = "kaito-local-nvme-disk"
)
