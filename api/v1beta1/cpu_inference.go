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

var (
	awsCPUInstanceTypePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*\.[a-z0-9]+$`)
	awsInstanceFamilyPattern  = regexp.MustCompile(`^([a-z]+)`)
)

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

// IsInferenceTemplateCPUTarget returns true when the custom template does not
// request GPU resources.
func IsInferenceTemplateCPUTarget(template *corev1.PodTemplateSpec) bool {
	if template == nil || len(template.Spec.Containers) == 0 {
		return false
	}

	for _, container := range template.Spec.Containers {
		if hasNVIDIAAcceleratorResource(container.Resources.Requests) || hasNVIDIAAcceleratorResource(container.Resources.Limits) {
			return false
		}
	}

	return true
}

// IsCPUCompatiblePreset reports whether the preset reference is eligible for the
// Phase 1 CPU preset path. Legal built-in presets and generic Hugging Face
// model IDs are allowed; full model resolution/compatibility is validated in the
// normal preset validation flow.
func IsCPUCompatiblePreset(name ModelName) bool {
	if name == "" {
		return false
	}

	return plugin.IsValidPreset(strings.ToLower(string(name)))
}

func isAzureGPUOrAcceleratorSKU(instanceType string) bool {
	return strings.HasPrefix(instanceType, "Standard_") && sku.HasSKUNamePrefix(instanceType, N_SERIES_PREFIX)
}

func isAWSGPUOrAcceleratorInstanceType(instanceType string) bool {
	if !awsCPUInstanceTypePattern.MatchString(instanceType) {
		return false
	}

	familyPart := strings.ToLower(strings.SplitN(instanceType, ".", 2)[0])
	matches := awsInstanceFamilyPattern.FindStringSubmatch(familyPart)
	if len(matches) < 2 {
		return false
	}

	switch matches[1] {
	case "g", "p", "inf", "trn", "dl":
		return true
	default:
		return false
	}
}

func isCPUInstanceType(instanceType string) bool {
	if instanceType == "" {
		return false
	}

	if strings.HasPrefix(instanceType, "Standard_") {
		return !isAzureGPUOrAcceleratorSKU(instanceType)
	}

	if !awsCPUInstanceTypePattern.MatchString(instanceType) {
		return false
	}

	return !isAWSGPUOrAcceleratorInstanceType(instanceType)
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

	return false
}

// AreLabelSelectedReadyNodesCPU reports whether the ready nodes matching the selector
// are uniformly CPU-only (no NVIDIA GPU signals).
func AreLabelSelectedReadyNodesCPU(ctx context.Context, labelSelector *metav1.LabelSelector, kubeClient client.Client) (bool, error) {
	if kubeClient == nil {
		return false, nil
	}

	var matchLabels client.MatchingLabels
	if labelSelector != nil {
		matchLabels = labelSelector.MatchLabels
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

		if nodeHasGPUSignals(node) {
			seenReadyGPU = true
		} else {
			seenReadyCPU = true
		}
		if seenReadyCPU && seenReadyGPU {
			return false, fmt.Errorf("labelSelector matches a mix of CPU-only and GPU nodes; use a selector that resolves to a homogeneous CPU node pool for Phase 1 CPU preset inference")
		}
	}

	return seenReadyCPU && !seenReadyGPU, nil
}

// IsAttemptingCPUPresetTarget reports whether the object is trying to use the
// preset CPU path by pairing a preset with a CPU instance type.
func IsAttemptingCPUPresetTarget(inference *InferenceSpec, instanceType string) bool {
	return inference != nil && inference.Preset != nil && isCPUInstanceType(instanceType)
}

// ResolveCPUInferenceTarget reports whether the inference spec resolves to the Phase 1 CPU path.
func ResolveCPUInferenceTarget(ctx context.Context, inference *InferenceSpec, instanceType string, labelSelector *metav1.LabelSelector, kubeClient client.Client) (bool, error) {
	if inference == nil {
		return false, nil
	}
	if IsInferenceTemplateCPUTarget(inference.Template) {
		return true, nil
	}
	if inference.Preset == nil || !IsCPUCompatiblePreset(inference.Preset.Name) {
		return false, nil
	}
	if isCPUInstanceType(instanceType) {
		return true, nil
	}
	if !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		return false, nil
	}
	return AreLabelSelectedReadyNodesCPU(ctx, labelSelector, kubeClient)
}

// IsCPUInferenceTarget reports whether the inference spec resolves to the Phase 1 CPU path.
func IsCPUInferenceTarget(inference *InferenceSpec, instanceType string) bool {
	if inference == nil {
		return false
	}
	if IsInferenceTemplateCPUTarget(inference.Template) {
		return true
	}
	return inference.Preset != nil && IsCPUCompatiblePreset(inference.Preset.Name) && isCPUInstanceType(instanceType)
}

// IsCPUInferenceWorkspace reports whether the workspace matches the Phase 1 CPU path.
func IsCPUInferenceWorkspace(ws *Workspace) bool {
	if ws == nil {
		return false
	}

	return IsCPUInferenceTarget(ws.Inference, ws.Resource.InstanceType)
}
