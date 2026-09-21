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
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// Finalizers
	ModelMirrorFinalizer = "kaito.sh/model-mirror-cleanup"

	// Annotations
	AnnotationModelStreaming          = "kaito.sh/model-streaming"
	AnnotationModelMirrorStorageClass = "kaito.sh/model-mirror-storage-class"
	AnnotationStreamingServiceAccount = "kaito.sh/streaming-service-account"

	// Conditions
	ConditionTypeStorageReady = "StorageReady"
	ConditionTypeReady        = "Ready"

	// Condition reasons.
	ReasonPVCBound          = "PVCBound"
	ReasonPVCPending        = "PVCPending"
	ReasonPVCCreateFailed   = "PVCCreateFailed"
	ReasonJobCreateFailed   = "JobCreateFailed"
	ReasonDownloadFailed    = "DownloadFailed"
	ReasonDownloadOOMKilled = "DownloadOOMKilled"
	ReasonDownloadEvicted   = "DownloadEvicted"
	ReasonDownloadSucceeded = "DownloadSucceeded"
	ReasonStaticMirror      = "StaticMirror"
	ReasonInvalidSpec       = "InvalidSpec"

	// Labels
	LabelModelMirrorName = "kaito.sh/model-mirror-name"

	// Downloader image
	DownloaderImage = "mcr.microsoft.com/mirror/docker/library/python:3.11-slim"

	// huggingface-hub version
	HuggingFaceHubVersion = "1.18.0"

	// prometheus-client version, used by the sampler sidecar to expose progress
	PrometheusClientVersion = "0.24.1"

	// Default requests reserve capacity that fits smaller system nodes, while higher
	// limits allow the four parallel download workers to use idle node resources.
	DefaultDownloadJobCPU         = "2"
	DefaultDownloadJobMemory      = "6Gi"
	DefaultDownloadJobCPULimit    = "4"
	DefaultDownloadJobMemoryLimit = "10Gi"
)

// DownloadExcludePatterns is the list of glob patterns to exclude from HF downloads.
var DownloadExcludePatterns = []string{"original/*"}

// DownloadJobResources holds the CPU/memory requests and limits applied to the download Job container.
type DownloadJobResources struct {
	CPU         string
	Memory      string
	CPULimit    string
	MemoryLimit string
}

func DefaultDownloadJobResources() DownloadJobResources {
	return DownloadJobResources{
		CPU:         DefaultDownloadJobCPU,
		Memory:      DefaultDownloadJobMemory,
		CPULimit:    DefaultDownloadJobCPULimit,
		MemoryLimit: DefaultDownloadJobMemoryLimit,
	}
}

// ResolveDownloadJobResources applies flag overrides and validates the resulting quantities.
func ResolveDownloadJobResources(cpu, memory, cpuLimit, memoryLimit string) (DownloadJobResources, error) {
	resources := DefaultDownloadJobResources()
	if cpu != "" {
		resources.CPU = cpu
		resources.CPULimit = cpu
	}
	if memory != "" {
		resources.Memory = memory
		resources.MemoryLimit = memory
	}
	if cpuLimit != "" {
		resources.CPULimit = cpuLimit
	}
	if memoryLimit != "" {
		resources.MemoryLimit = memoryLimit
	}

	if err := validateRequestAndLimit("CPU", resources.CPU, resources.CPULimit); err != nil {
		return DownloadJobResources{}, err
	}
	if err := validateRequestAndLimit("memory", resources.Memory, resources.MemoryLimit); err != nil {
		return DownloadJobResources{}, err
	}
	return resources, nil
}

func validateRequestAndLimit(name, requestValue, limitValue string) error {
	request, err := resource.ParseQuantity(requestValue)
	if err != nil {
		return fmt.Errorf("invalid ModelMirror download %s request %q: %w", name, requestValue, err)
	}
	limit, err := resource.ParseQuantity(limitValue)
	if err != nil {
		return fmt.Errorf("invalid ModelMirror download %s limit %q: %w", name, limitValue, err)
	}
	if request.Sign() <= 0 {
		return fmt.Errorf("ModelMirror download %s request must be greater than zero, got %q", name, requestValue)
	}
	if limit.Sign() <= 0 {
		return fmt.Errorf("ModelMirror download %s limit must be greater than zero, got %q", name, limitValue)
	}
	if limit.Cmp(request) < 0 {
		return fmt.Errorf("ModelMirror download %s limit %q must be greater than or equal to request %q", name, limitValue, requestValue)
	}
	return nil
}
