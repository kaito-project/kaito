// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package consts

import "time"

const (
	// WorkspaceFinalizer is used to make sure that workspace controller handles garbage collection.
	WorkspaceFinalizer = "workspace.finalizer.kaito.sh"
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
	NvidiaGPU                     = "nvidia.com/gpu"

	// Feature flags
	FeatureFlagVLLM            = "vLLM"
	FeatureFlagEnsureNodeClass = "ensureNodeClass"

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
)

var (
	LocalNVMeStorageClass = "kaito-local-nvme-disk"
)
