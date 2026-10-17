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

package nodeclaim

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestGenerateNodeClaimManifestWithOptions_CPUWorkspaceUsesCPUTaint(t *testing.T) {
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
	}()
	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true

	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	one := 1
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-ws", Namespace: "default"},
		Resource: kaitov1beta1.ResourceSpec{
			Count:        &one,
			InstanceType: "Standard_D8s_v5",
		},
		Inference: &kaitov1beta1.InferenceSpec{
			Template: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "cpu", Image: "example.com/cpu:latest"}},
				},
			},
		},
	}

	nc := GenerateNodeClaimManifestWithOptions("0", ws, ManifestOptions{})
	if nc == nil {
		t.Fatalf("expected nodeclaim manifest")
	}
	if len(nc.Spec.Taints) != 1 {
		t.Fatalf("expected one CPU taint for cpu workspace, got %v", nc.Spec.Taints)
	}
	if nc.Spec.Taints[0].Key != consts.SKUString || nc.Spec.Taints[0].Value != consts.CPUString || nc.Spec.Taints[0].Effect != corev1.TaintEffectNoSchedule {
		t.Fatalf("expected cpu sku taint, got %v", nc.Spec.Taints)
	}
	if got := nc.Labels[consts.SKUString]; got != consts.CPUString {
		t.Fatalf("expected cpu sku label, got %q", got)
	}
}
