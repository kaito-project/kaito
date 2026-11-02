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

package v1alpha1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	beta "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	testutils "github.com/kaito-project/kaito/pkg/utils/test"
)

func TestServedAlphaCPUWorkspaceValidation(t *testing.T) {
	testutils.RegisterTestModel()
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	t.Setenv("RELEASE_NAMESPACE", "default")
	oldCPU, oldVLLM, oldNAP, oldClient := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference], featuregates.FeatureGates[consts.FeatureFlagVLLM], featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning], k8sclient.Client
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference], featuregates.FeatureGates[consts.FeatureFlagVLLM], featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning], k8sclient.Client = oldCPU, oldVLLM, oldNAP, oldClient
	})
	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference], featuregates.FeatureGates[consts.FeatureFlagVLLM], featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true, true, true
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	k8sclient.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: DefaultInferenceConfigTemplate, Namespace: "default"}, Data: map[string]string{"inference_config.yaml": "vllm:\n  max-model-len: \"2048\"\n"}}).Build()
	for _, tc := range []struct {
		name, instance, runtime, want string
		count                         int
	}{
		{"amd64", "Standard_D8s_v5", "vllm", "", 1},
		{"explicit transformers", "Standard_D8s_v5", "transformers", "", 1},
		{"unsupported runtime", "Standard_D8s_v5", "other", "CPU inference requires", 1},
		{"arm", "Standard_D8ps_v6", "vllm", "only supports amd64", 1},
		{"multiple nodes", "Standard_D8s_v5", "vllm", "single node", 2},
	} {
		for _, update := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/create", true: "/update"}[update], func(t *testing.T) {
				ws := &Workspace{ObjectMeta: metav1.ObjectMeta{Name: "cpu-test", Namespace: "default", Annotations: map[string]string{AnnotationWorkspaceRuntime: tc.runtime}}, Resource: ResourceSpec{InstanceType: tc.instance, Count: &tc.count, LabelSelector: &metav1.LabelSelector{}}, Inference: &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}}}
				ctx := context.Background()
				if update {
					ctx = apis.WithinUpdate(ctx, ws.DeepCopy())
				}
				err := ws.Validate(ctx)
				if tc.want != "" {
					require.NotNil(t, err)
					require.Contains(t, err.Error(), tc.want)
				} else {
					require.Nil(t, err)
				}
			})
		}
	}
}

func TestServedAlphaInferenceSetCPUChildValidation(t *testing.T) {
	testutils.RegisterTestModel()
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	t.Setenv("RELEASE_NAMESPACE", "default")
	oldCPU, oldVLLM := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference], featuregates.FeatureGates[consts.FeatureFlagVLLM]
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference], featuregates.FeatureGates[consts.FeatureFlagVLLM] = oldCPU, oldVLLM
	})
	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference], featuregates.FeatureGates[consts.FeatureFlagVLLM] = true, true
	for _, tc := range []struct{ name, instance, runtime, want string }{
		{"amd64", "Standard_D8s_v5", "vllm", ""},
		{"explicit transformers", "Standard_D8s_v5", "transformers", ""},
		{"arm", "Standard_D8ps_v6", "vllm", "only supports amd64"},
		{"runtime", "Standard_D8s_v5", "other", "CPU inference requires"},
	} {
		for _, update := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/create", true: "/update"}[update], func(t *testing.T) {
				is := &InferenceSet{ObjectMeta: metav1.ObjectMeta{Name: "cpu-set", Namespace: "default"}, Spec: InferenceSetSpec{Template: InferenceSetTemplate{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{AnnotationWorkspaceRuntime: tc.runtime}}, Resource: InferenceSetResourceSpec{InstanceType: tc.instance}, Inference: beta.InferenceSpec{Preset: &beta.PresetSpec{PresetMeta: beta.PresetMeta{Name: "test-model"}}}}}}
				ctx := context.Background()
				if update {
					ctx = apis.WithinUpdate(ctx, is.DeepCopy())
				}
				err := is.Validate(ctx)
				if tc.want != "" {
					require.NotNil(t, err)
					require.Contains(t, err.Error(), tc.want)
					require.Contains(t, err.Error(), "spec.template")
				} else {
					require.Nil(t, err)
				}
			})
		}
	}
}
