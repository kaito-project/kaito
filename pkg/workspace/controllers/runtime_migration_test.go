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
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestLegacyRuntimeMarkerMigrationPreservesPodTemplate(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	test.RegisterTestModel()
	for _, gate := range []string{consts.FeatureFlagVLLM, consts.FeatureFlagEnableCPUInference} {
		old := featuregates.FeatureGates[gate]
		t.Cleanup(func() { featuregates.FeatureGates[gate] = old })
	}
	featuregates.FeatureGates[consts.FeatureFlagVLLM] = true
	featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = false
	for _, engine := range []string{"vllm", "transformers", "unknown"} {
		t.Run(engine, func(t *testing.T) {
			ws := test.MockWorkspaceWithPresetVLLM.DeepCopy()
			ws.Annotations = map[string]string{}
			if engine == "transformers" {
				ws.Annotations[v1beta1.AnnotationWorkspaceRuntime] = engine
			}
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, appsv1.AddToScheme(scheme))
			require.NoError(t, v1beta1.AddToScheme(scheme))
			seedCompletedBenchmark(ws)
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(ws).WithObjects(ws).Build()
			r := &WorkspaceReconciler{Client: c}
			require.NoError(t, r.applyInference(context.Background(), ws))
			deployed := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(ws), deployed))
			delete(deployed.Annotations, v1beta1.AnnotationWorkspaceRuntime)
			deployed.Spec.Template.Annotations = map[string]string{"custom": "keep"}
			spec := &deployed.Spec.Template.Spec
			spec.Containers[0].Image = "mcr.microsoft.com/kaito/kaito-base:deployed-old"
			spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("1234m")
			spec.Containers[0].StartupProbe = &corev1.Probe{InitialDelaySeconds: 999}
			spec.Containers[0].ReadinessProbe = &corev1.Probe{InitialDelaySeconds: 888}
			spec.Containers[0].LivenessProbe = &corev1.Probe{InitialDelaySeconds: 777}
			spec.Tolerations = append(spec.Tolerations, corev1.Toleration{Key: "custom", Operator: corev1.TolerationOpExists})
			spec.Affinity = &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "custom", Operator: corev1.NodeSelectorOpIn, Values: []string{"keep"}}}}}}}}
			spec.Containers = append(spec.Containers, corev1.Container{Name: "custom-sidecar", Image: "custom:keep", Command: []string{"python", "/workspace/vllm/inference_api.py"}})
			spec.InitContainers = append(spec.InitContainers, corev1.Container{Name: "custom-init", Image: "custom-init:keep"})
			if engine == "unknown" {
				spec.Containers[0].Command = []string{"custom-launcher"}
				spec.Containers[0].Args = nil
				spec.Containers[0].Env = nil
			}
			require.NoError(t, c.Update(context.Background(), deployed))
			before := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(ws), before))
			require.NoError(t, r.applyInference(context.Background(), ws))
			after := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(ws), after))
			require.Equal(t, before.Spec, after.Spec, "marker migration must not roll out or reset any PodSpec field")
			requireBenchmarkStatus(t, c, ws, false)
			if engine == "unknown" {
				require.NotContains(t, after.Annotations, v1beta1.AnnotationWorkspaceRuntime)
			} else {
				require.Equal(t, engine, after.Annotations[v1beta1.AnnotationWorkspaceRuntime])
			}
			require.NoError(t, r.applyInference(context.Background(), ws))
			again := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(ws), again))
			require.Equal(t, after, again, "a second reconcile must be idempotent")
			requireBenchmarkStatus(t, c, ws, false)
		})
	}
}

func TestLegacyRuntimeSwitchWithoutMarker(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	test.RegisterTestModel()
	old := featuregates.FeatureGates[consts.FeatureFlagVLLM]
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagVLLM] = old })
	featuregates.FeatureGates[consts.FeatureFlagVLLM] = true
	for _, direction := range []struct{ from, to string }{{"vllm", "transformers"}, {"transformers", "vllm"}} {
		t.Run(direction.from+"-to-"+direction.to, func(t *testing.T) {
			ws := test.MockWorkspaceWithPresetVLLM.DeepCopy()
			ws.Annotations = map[string]string{v1beta1.AnnotationWorkspaceRuntime: direction.from}
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, appsv1.AddToScheme(scheme))
			require.NoError(t, v1beta1.AddToScheme(scheme))
			seedCompletedBenchmark(ws)
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(ws).WithObjects(ws).Build()
			r := &WorkspaceReconciler{Client: c}
			require.NoError(t, r.applyInference(context.Background(), ws))
			before := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(ws), before))
			delete(before.Annotations, v1beta1.AnnotationWorkspaceRuntime)
			before.Spec.Template.Spec.Containers[0].Image = "mcr.microsoft.com/kaito/kaito-base:old"
			before.Spec.Template.Spec.Containers[0].StartupProbe = &corev1.Probe{InitialDelaySeconds: 999}
			before.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{InitialDelaySeconds: 888}
			require.NoError(t, c.Update(context.Background(), before))
			ws.Annotations[v1beta1.AnnotationWorkspaceRuntime] = direction.to
			require.NoError(t, r.applyInference(context.Background(), ws))
			after := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(ws), after))
			require.Equal(t, direction.to, after.Annotations[v1beta1.AnnotationWorkspaceRuntime])
			require.Equal(t, pkgmodel.RuntimeName(direction.to), inferDeployedRuntime(after))
			require.NotEqual(t, before.Spec.Template.Spec.Containers[0].StartupProbe, after.Spec.Template.Spec.Containers[0].StartupProbe)
			require.NotEqual(t, before.Spec.Template.Spec.Containers[0].ReadinessProbe, after.Spec.Template.Spec.Containers[0].ReadinessProbe)
			require.Equal(t, "mcr.microsoft.com/kaito/kaito-base:old", after.Spec.Template.Spec.Containers[0].Image)
			requireBenchmarkStatus(t, c, ws, true)
		})
	}
}

func TestInferDeployedRuntimeEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		serving corev1.Container
		want    pkgmodel.RuntimeName
	}{
		{"vllm command", corev1.Container{Command: []string{"python /workspace/vllm/inference_api.py"}}, pkgmodel.RuntimeNameVLLM},
		{"transformers args", corev1.Container{Command: []string{"accelerate"}, Args: []string{"launch", "/workspace/tfs/inference_api.py"}}, pkgmodel.RuntimeNameHuggingfaceTransformers},
		{"conflicting scripts", corev1.Container{Command: []string{"/workspace/vllm/inference_api.py /workspace/tfs/inference_api.py"}}, ""},
		{"shared GPU image unknown", corev1.Container{Image: "registry:5000/kaito-base:0.0.1"}, ""},
		{"CPU vLLM image", corev1.Container{Image: "registry:5000/kaito-base-vllm-cpu:0.0.2"}, pkgmodel.RuntimeNameVLLM},
		{"explicit vLLM env", corev1.Container{Env: []corev1.EnvVar{{Name: "VLLM_TARGET_DEVICE", Value: "cpu"}}}, pkgmodel.RuntimeNameVLLM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.serving.Name = "ws"
			ss := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "ws"}, Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{tc.serving, {Name: "sidecar", Command: []string{"/workspace/vllm/inference_api.py"}}}}}}}
			require.Equal(t, tc.want, inferDeployedRuntime(ss))
		})
	}
	require.Empty(t, inferDeployedRuntime(&appsv1.StatefulSet{}))
}
