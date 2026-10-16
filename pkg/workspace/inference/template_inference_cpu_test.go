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
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestCreateTemplateInference_CPUWorkspaceOmitsDefaultGPUTolerations(t *testing.T) {
	oldGate := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldGate
	}()
	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = true

	one := 1
	workspace := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-ws", Namespace: "default"},
		Resource: kaitov1beta1.ResourceSpec{
			Count:         &one,
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

	mockClient := test.NewClient()
	mockClient.On("Create", mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)

	obj, err := CreateTemplateInference(context.Background(), workspace, mockClient, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ss, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		t.Fatalf("expected StatefulSet, got %T", obj)
	}
	if len(ss.Spec.Template.Spec.Tolerations) != 0 {
		t.Fatalf("expected cpu template inference to omit default gpu tolerations, got %v", ss.Spec.Template.Spec.Tolerations)
	}
}
