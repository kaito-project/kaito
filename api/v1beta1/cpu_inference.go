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

package v1beta1

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/mig"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

// Azure CPU SKUs include both unversioned names (e.g. Standard_B2s) and versioned names.
// CPU classification is architecture-agnostic; it does not establish image compatibility.
var azureCPUInstancePattern = regexp.MustCompile(`(?i)^standard_[a-z]+[0-9]+(?:-[0-9]+)?[a-z]*(?:_v[0-9]+)?$`)

func hasNVIDIAAcceleratorResource(resources corev1.ResourceList) bool {
	for resourceName, qty := range resources {
		if qty.IsZero() {
			continue
		}
		name := string(resourceName)
		if name == consts.NvidiaGPU || strings.HasPrefix(name, mig.MIGResourcePrefix) {
			return true
		}
	}
	return false
}

// IsCPUCompatiblePreset reports whether the preset reference is eligible for the
// Basic CPU preset path. Legal built-in presets and generic Hugging Face
// model IDs are allowed; full model resolution/compatibility is validated in the
// normal preset validation flow.
func IsCPUCompatiblePreset(name ModelName) bool {
	if name == "" {
		return false
	}

	return plugin.IsValidPreset(strings.ToLower(string(name)))
}

func isAzureGPUOrAcceleratorSKU(instanceType string) bool {
	return strings.HasPrefix(strings.ToLower(instanceType), "standard_") && sku.HasSKUNamePrefix(instanceType, N_SERIES_PREFIX)
}

func isAzureCPUInstanceType(instanceType string) bool {
	return azureCPUInstancePattern.MatchString(instanceType) && !isAzureGPUOrAcceleratorSKU(instanceType)
}

func nodeHasGPUSignals(node *corev1.Node) bool {
	if node == nil {
		return false
	}

	if hasNVIDIAAcceleratorResource(node.Status.Capacity) {
		return true
	}

	for _, key := range []string{consts.NvidiaGPUProduct, consts.NvidiaGPUCount, consts.NvidiaGPUMemory} {
		if value := strings.TrimSpace(node.Labels[key]); value != "" {
			return true
		}
	}

	if instanceType := strings.TrimSpace(node.Labels[corev1.LabelInstanceTypeStable]); instanceType != "" {
		if gpuConfig, _ := sku.GetGPUConfigBySKU(instanceType); gpuConfig != nil {
			return true
		}
	}

	return false
}

// AreLabelSelectedReadyNodesCPU reports whether the ready nodes matching the selector
// and effective instance-type filter are uniformly CPU-only (no NVIDIA GPU signals).
func AreLabelSelectedReadyNodesCPU(ctx context.Context, labelSelector *metav1.LabelSelector, instanceType string, kubeClient client.Client) (bool, error) {
	if kubeClient == nil {
		return false, nil
	}

	var matchLabels client.MatchingLabels
	if labelSelector != nil {
		matchLabels = SanitizedMatchLabels(labelSelector)
	}

	nodeList := &corev1.NodeList{}
	if err := kubeClient.List(ctx, nodeList, matchLabels); err != nil {
		return false, err
	}

	seenReadyCPU := false
	seenReadyGPU := false
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if node.DeletionTimestamp != nil {
			continue
		}
		if instanceType != "" && node.Labels[corev1.LabelInstanceTypeStable] != instanceType {
			continue
		}

		isReady := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				isReady = true
				break
			}
		}
		if !isReady {
			continue
		}

		if !nodeHasGPUSignals(node) && strings.HasPrefix(node.Spec.ProviderID, "aws://") {
			return false, fmt.Errorf("AWS CPU nodes are not supported for basic CPU inference")
		}

		if nodeHasGPUSignals(node) {
			seenReadyGPU = true
		} else {
			seenReadyCPU = true
		}
		if seenReadyCPU && seenReadyGPU {
			return false, fmt.Errorf("labelSelector matches a mix of CPU-only and GPU nodes; use a selector that resolves to a homogeneous CPU node pool for basic CPU preset inference")
		}
	}

	return seenReadyCPU && !seenReadyGPU, nil
}

// ResolveCPUInferenceTarget reports whether the inference spec resolves to the basic CPU path.
func ResolveCPUInferenceTarget(ctx context.Context, inference *InferenceSpec, instanceType string, labelSelector *metav1.LabelSelector, kubeClient client.Client) (bool, error) {
	if os.Getenv("CLOUD_PROVIDER") == consts.AWSCloudName || inference == nil {
		return false, nil
	}
	// Custom templates never infer CPU intent from selected nodes.
	if inference.Template != nil || instanceType != "" {
		return IsCPUInferenceTarget(inference, instanceType), nil
	}
	// Only label-only BYO presets need node-based resolution.
	if inference.Preset == nil || !IsCPUCompatiblePreset(inference.Preset.Name) ||
		!featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] ||
		len(SanitizedMatchLabels(labelSelector)) == 0 {
		return false, nil
	}
	return AreLabelSelectedReadyNodesCPU(ctx, labelSelector, "", kubeClient)
}

// IsCPUInferenceTarget reports whether the inference spec resolves to the basic CPU path.
func IsCPUInferenceTarget(inference *InferenceSpec, instanceType string) bool {
	if os.Getenv("CLOUD_PROVIDER") == consts.AWSCloudName {
		return false
	}
	if inference == nil {
		return false
	}
	if inference.Template != nil {
		return isAzureCPUInstanceType(instanceType)
	}
	return inference.Preset != nil && IsCPUCompatiblePreset(inference.Preset.Name) && isAzureCPUInstanceType(instanceType)
}

// IsExplicitTransformersCPUWorkspace permits the existing Transformers base image
// only for an explicitly selected runtime and a declared CPU VM instance type.
// Label-only BYO targets retain the vLLM-only CPU contract.
func IsExplicitTransformersCPUWorkspace(ws *Workspace) bool {
	return ws != nil && ws.Inference != nil && ws.Inference.Preset != nil &&
		ws.GetAnnotations()[AnnotationWorkspaceRuntime] == "transformers" &&
		IsCPUInferenceTarget(ws.Inference, ws.Resource.InstanceType)
}

// IsCPUInferenceWorkspace reports whether the workspace matches the basic CPU path.
func IsCPUInferenceWorkspace(ws *Workspace) bool {
	if ws == nil {
		return false
	}

	return IsCPUInferenceTarget(ws.Inference, ws.Resource.InstanceType)
}

// ResolveCPUInferenceWorkspace reports whether the workspace resolves to the basic CPU path,
// including preset BYO selector-based detection when instanceType is empty.
func ResolveCPUInferenceWorkspace(ctx context.Context, ws *Workspace, kubeClient client.Client) (bool, error) {
	if ws == nil {
		return false, nil
	}
	return ResolveCPUInferenceTarget(ctx, ws.Inference, ws.Resource.InstanceType, ws.Resource.LabelSelector, kubeClient)
}

// ResolveCPUInferenceSetTarget reports whether the InferenceSet template resolves to the
// basic CPU path, including preset BYO selector-based detection when instanceType is empty.
func ResolveCPUInferenceSetTarget(ctx context.Context, iObj *InferenceSet, kubeClient client.Client) (bool, error) {
	if iObj == nil {
		return false, nil
	}
	return ResolveCPUInferenceTarget(ctx, &iObj.Spec.Template.Inference, iObj.Spec.Template.Resource.InstanceType, InferenceSetNodeSelector(iObj), kubeClient)
}

// InferenceSetNodeSelector returns the node selector used by child Workspaces.
// The top-level selector selects Workspaces; it is only a legacy node selector
// fallback when automatic node provisioning is enabled.
func InferenceSetNodeSelector(iObj *InferenceSet) *metav1.LabelSelector {
	selector := iObj.Spec.Template.Resource.LabelSelector
	if IsEmptyLabelSelector(selector) && !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		return iObj.Spec.Selector
	}
	return selector
}
