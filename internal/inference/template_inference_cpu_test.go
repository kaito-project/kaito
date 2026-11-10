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

package inference

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/internal/testutil"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestCreateTemplateInferenceDoesNotSelectCPUPresetBehavior(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	one := 1
	workspace := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-ws", Namespace: "default"},
		Resource: kaitov1beta1.ResourceSpec{
			Count:         &one,
			InstanceType:  "Standard_D8s_v5",
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kaito.sh/cpu-inference": "true"}},
		},
		Inference: &kaitov1beta1.InferenceSpec{
			Template: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "cpu", Image: "example.com/cpu:latest"}},
				},
			},
		},
		Status: kaitov1beta1.WorkspaceStatus{TargetNodeCount: 1},
	}

	mockClient := testutil.NewClient()
	mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(testutil.NotFoundError())
	mockClient.On("Create", mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)

	obj, err := CreateTemplateInference(context.Background(), workspace, mockClient, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ss, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		t.Fatalf("expected StatefulSet, got %T", obj)
	}
	hasGPU := false
	hasCPU := false
	for _, toleration := range ss.Spec.Template.Spec.Tolerations {
		if toleration.Key == consts.SKUString && toleration.Value == consts.GPUString {
			hasGPU = true
		}
		if toleration.Key == consts.SKUString && toleration.Value == consts.CPUString {
			hasCPU = true
		}
	}
	if !hasGPU {
		t.Fatalf("expected custom template inference to preserve default gpu tolerations, got %v", ss.Spec.Template.Spec.Tolerations)
	}
	if hasCPU {
		t.Fatalf("expected custom template inference not to add CPU preset tolerations, got %v", ss.Spec.Template.Spec.Tolerations)
	}
}
