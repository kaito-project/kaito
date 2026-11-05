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
	"os"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// Azure CPU names may be versioned or unversioned. The p suffix denotes ARM;
// the CPU preset image is x86_64-only. N-series are accelerator targets even
// when their GPU configuration is not in the local SKU catalog.
var azureCPUInstancePattern = regexp.MustCompile(`(?i)^standard_[a-z]+[0-9]+(?:-[0-9]+)?[a-z]*(?:_v[0-9]+)?$`)
var azureARMInstancePattern = regexp.MustCompile(`(?i)^standard_[a-z]+[0-9]+(?:-[0-9]+)?[a-z]*p[a-z]*(?:_v[0-9]+)?$`)

func isAzureGPUOrAcceleratorSKU(instanceType string) bool {
	return strings.HasPrefix(strings.ToLower(instanceType), "standard_") && sku.HasSKUNamePrefix(instanceType, N_SERIES_PREFIX)
}

func isAzureCPUInstanceType(instanceType string) bool {
	return azureCPUInstancePattern.MatchString(instanceType) &&
		!isAzureGPUOrAcceleratorSKU(instanceType) && !azureARMInstancePattern.MatchString(instanceType)
}

// IsCPUInferenceTarget selects only presets with an explicit non-GPU, non-ARM
// Azure instance type. Node labels and custom templates never imply CPU intent.
// Runtime compatibility is checked by admission and workload generation.
func IsCPUInferenceTarget(inference *InferenceSpec, instanceType string) bool {
	return os.Getenv("CLOUD_PROVIDER") != consts.AWSCloudName && inference != nil &&
		inference.Template == nil && inference.Preset != nil &&
		isAzureCPUInstanceType(instanceType)
}

func IsCPUInferenceWorkspace(ws *Workspace) bool {
	return ws != nil && IsCPUInferenceTarget(ws.Inference, ws.Resource.InstanceType)
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
