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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestGenerateNodeClaimManifestWithOptions_CPUWorkspaceOmitsGPUTaint(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	one := 1
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-ws", Namespace: "default"},
		Resource: kaitov1beta1.ResourceSpec{
			Count:        &one,
			InstanceType: "Standard_D8s_v5",
		},
		Inference: &kaitov1beta1.InferenceSpec{
			Preset: &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: kaitov1beta1.ModelName("test-model")}},
		},
	}

	nc := GenerateNodeClaimManifestWithOptions("", ws, ManifestOptions{})
	if nc == nil {
		t.Fatalf("expected nodeclaim manifest")
	}
	if len(nc.Spec.Taints) != 0 {
		t.Fatalf("expected no GPU taint for cpu workspace, got %v", nc.Spec.Taints)
	}
}
