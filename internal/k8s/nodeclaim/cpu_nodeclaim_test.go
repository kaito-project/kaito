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
	"github.com/kaito-project/kaito/pkg/consts"
)

func TestGenerateNodeClaimManifestWithOptions_CPUWorkspaceUsesCPUTaint(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	one := 1
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-ws", Namespace: "default"},
		Resource: kaitov1beta1.ResourceSpec{
			Count:        &one,
			InstanceType: "Standard_D8s_v5",
		},
		Inference: &kaitov1beta1.InferenceSpec{Preset: &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: "test-model"}}},
	}

	nc := GenerateNodeClaimManifestWithOptions("0", ws, ManifestOptions{})
	if nc == nil {
		t.Fatalf("expected nodeclaim manifest")
	}
	if len(nc.Spec.Taints) != 1 {
		t.Fatalf("expected one CPU taint for cpu workspace, got %v", nc.Spec.Taints)
	}
	if nc.Spec.Taints[0].Key != kaitov1beta1.LabelSKU || nc.Spec.Taints[0].Value != kaitov1beta1.SKUCPU || nc.Spec.Taints[0].Effect != corev1.TaintEffectNoSchedule {
		t.Fatalf("expected cpu sku taint, got %v", nc.Spec.Taints)
	}
	if got := nc.Labels[kaitov1beta1.LabelSKU]; got != kaitov1beta1.SKUCPU {
		t.Fatalf("expected cpu sku label, got %q", got)
	}
}

func TestAWSNodeClaimPreservesGPURequirement(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AWSCloudName)
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-ws", Namespace: "default"},
		Resource:   kaitov1beta1.ResourceSpec{InstanceType: "g5.xlarge"},
		Inference:  &kaitov1beta1.InferenceSpec{Template: &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "custom", Image: "example.com/custom:v1"}}}}},
	}
	nc := GenerateNodeClaimManifest("0", ws)
	if nc == nil {
		t.Fatal("expected AWS GPU NodeClaim")
	}
	if len(nc.Spec.Taints) != 1 || nc.Spec.Taints[0].Value != kaitov1beta1.SKUGPU {
		t.Fatalf("expected GPU taint, got %v", nc.Spec.Taints)
	}
	for _, req := range nc.Spec.Requirements {
		if req.Key == "karpenter.k8s.aws/instance-gpu-count" && req.Operator == corev1.NodeSelectorOpGt && len(req.Values) == 1 && req.Values[0] == "0" {
			return
		}
	}
	t.Fatal("missing AWS GPU count requirement")
}
