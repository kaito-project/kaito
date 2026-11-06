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

package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestCPUInferenceCreateAndUnchangedReconcile(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	t.Setenv("PRESET_REGISTRY_NAME", "mcr.microsoft.com/aks/kaito")
	test.RegisterTestModel()
	for _, gate := range []string{consts.FeatureFlagVLLM, consts.FeatureFlagDisableNodeAutoProvisioning} {
		old := featuregates.FeatureGates[gate]
		t.Cleanup(func() { featuregates.FeatureGates[gate] = old })
	}
	featuregates.FeatureGates[consts.FeatureFlagVLLM] = true
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
	for _, runtimeName := range []string{"vllm"} {
		t.Run(runtimeName, func(t *testing.T) {
			ws := test.MockWorkspaceWithPresetVLLM.DeepCopy()
			ws.Resource.InstanceType = "Standard_D8s_v5"
			ws.Status.TargetNodeCount = 1
			ws.Annotations = map[string]string{v1beta1.AnnotationWorkspaceRuntime: runtimeName, v1beta1.WorkspaceRevisionAnnotation: "1"}
			c := fake.NewClientBuilder().WithScheme(benchmarkTestScheme(t)).WithStatusSubresource(ws).WithObjects(ws).Build()
			r := &WorkspaceReconciler{Client: c}
			require.NoError(t, r.applyInference(t.Context(), ws))
			ss := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ws), ss))
			require.NotContains(t, ss.Annotations, v1beta1.AnnotationWorkspaceRuntime)
			require.EqualValues(t, 1, *ss.Spec.Replicas)
			require.NotContains(t, ss.Spec.Template.Spec.Containers[0].Resources.Requests, corev1.ResourceName("nvidia.com/gpu"))
			require.Contains(t, ss.Spec.Template.Spec.Containers[0].Image, "kaito-base-cpu:")
			// An unchanged reconcile must preserve a deployed tag and local customizations.
			ss.Spec.Template.Spec.Containers[0].Image = "example.com/kaito:deployed"
			ss.Spec.Template.Spec.Containers[0].StartupProbe = &corev1.Probe{InitialDelaySeconds: 999}
			require.NoError(t, c.Update(t.Context(), ss))
			before := ss.DeepCopy()
			require.NoError(t, r.applyInference(t.Context(), ws))
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ws), ss))
			require.Equal(t, before, ss)
		})
	}
}

func TestWorkspaceHashPreservesUpstreamInputs(t *testing.T) {
	ws := test.MockWorkspaceWithPresetVLLM.DeepCopy()
	hasher := sha256.New()
	encoder := json.NewEncoder(hasher)
	require.NoError(t, encoder.Encode(ws.Resource))
	require.NoError(t, encoder.Encode(ws.Inference))
	require.NoError(t, encoder.Encode(ws.Tuning))
	want := hex.EncodeToString(hasher.Sum(nil))
	for _, runtimeName := range []string{"", "vllm", "transformers"} {
		ws.Annotations = map[string]string{v1beta1.AnnotationWorkspaceRuntime: runtimeName}
		require.Equal(t, want, ComputeHash(ws))
	}
}

// Replica drift alone must not expand reconciliation into a template rollout.
// This also protects existing GPU workloads.
func TestInferenceMatchingRevisionPreservesReplicaDrift(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	t.Setenv("PRESET_REGISTRY_NAME", "mcr.microsoft.com/aks/kaito")
	test.RegisterTestModel()
	for _, gate := range []string{consts.FeatureFlagVLLM, consts.FeatureFlagDisableNodeAutoProvisioning} {
		old := featuregates.FeatureGates[gate]
		t.Cleanup(func() { featuregates.FeatureGates[gate] = old })
	}
	featuregates.FeatureGates[consts.FeatureFlagVLLM] = true
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
	for _, tc := range []struct {
		name, instanceType string
	}{
		{"GPU", "Standard_NC24ads_A100_v4"},
		{"CPU", "Standard_D8s_v5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := test.MockWorkspaceWithPresetVLLM.DeepCopy()
			ws.Resource.InstanceType = tc.instanceType
			ws.Status.TargetNodeCount = 1
			ws.Annotations = map[string]string{v1beta1.AnnotationWorkspaceRuntime: "vllm", v1beta1.WorkspaceRevisionAnnotation: "1"}
			seedCompletedBenchmark(ws)
			c := fake.NewClientBuilder().WithScheme(benchmarkTestScheme(t)).WithStatusSubresource(ws).WithObjects(ws).Build()
			r := &WorkspaceReconciler{Client: c}
			require.NoError(t, r.applyInference(t.Context(), ws))
			ss := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ws), ss))
			require.EqualValues(t, 1, *ss.Spec.Replicas)
			*ss.Spec.Replicas = 3
			ss.Spec.Template.Spec.Containers[0].Image = "example.com/kaito:deployed"
			ss.Spec.Template.Spec.Containers[0].StartupProbe = &corev1.Probe{InitialDelaySeconds: 999}
			require.NoError(t, c.Update(t.Context(), ss))
			before := ss.DeepCopy()
			require.NoError(t, r.applyInference(t.Context(), ws))
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ws), ss))
			require.Equal(t, before, ss)
			requireBenchmarkStatus(t, c, ws, false)
		})
	}
}
