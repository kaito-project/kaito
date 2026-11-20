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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/internal/featuregates"
	"github.com/kaito-project/kaito/internal/manifests/builder"
	"github.com/kaito-project/kaito/internal/testutil"
	"github.com/kaito-project/kaito/pkg/accelerator/nvidia"
	"github.com/kaito-project/kaito/pkg/consts"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
)

func TestIsVLLMPreloadEligible(t *testing.T) {
	enabled := func() *v1beta1.Workspace {
		ws := testutil.MockWorkspaceWithPresetVLLM.DeepCopy()
		ws.Annotations = map[string]string{v1beta1.AnnotationEnableVLLMPreload: "true"}
		return ws
	}
	mig := enabled()
	mig.Resource.Partition = &v1beta1.PartitionSpec{Mode: v1beta1.PartitionModeMIG, Profile: "1g.10gb"}
	off := enabled()
	off.Annotations[v1beta1.AnnotationEnableVLLMPreload] = "false"

	tests := []struct {
		name        string
		ws          *v1beta1.Workspace
		runtime     pkgmodel.RuntimeName
		cpu         bool
		distributed bool
		streaming   string
		want        bool
	}{
		{name: "enabled single-node vllm", ws: enabled(), runtime: pkgmodel.RuntimeNameVLLM, want: true},
		{name: "annotation off", ws: off, runtime: pkgmodel.RuntimeNameVLLM},
		{name: "transformers", ws: enabled(), runtime: pkgmodel.RuntimeNameHuggingfaceTransformers},
		{name: "cpu", ws: enabled(), runtime: pkgmodel.RuntimeNameVLLM, cpu: true},
		{name: "multi-node", ws: enabled(), runtime: pkgmodel.RuntimeNameVLLM, distributed: true},
		{name: "mig", ws: mig, runtime: pkgmodel.RuntimeNameVLLM},
		{name: "streamed weights", ws: enabled(), runtime: pkgmodel.RuntimeNameVLLM, streaming: "az://models/phi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := isVLLMPreloadEligible(tt.ws, tt.runtime, tt.cpu, tt.distributed, tt.streaming)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want, reason == "")
		})
	}
}

func TestInjectVLLMPreloadSidecar(t *testing.T) {
	gpu := corev1.ResourceName("nvidia.com/gpu")
	spec := &corev1.PodSpec{Containers: []corev1.Container{{
		Name:  "ws",
		Image: "vllm-image",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{gpu: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("1Gi")},
			Limits:   corev1.ResourceList{gpu: resource.MustParse("2")},
		},
		Env:          []corev1.EnvVar{{Name: "HF_TOKEN", Value: "x"}, {Name: "NVIDIA_DRIVER_CAPABILITIES", Value: "compute"}},
		VolumeMounts: []corev1.VolumeMount{{Name: "weights", MountPath: "/workspace/weights"}},
	}}}

	injectVLLMPreloadSidecar(spec, "ws", []string{"/bin/sh", "-c", "preload"}, 20*time.Minute)

	require.Len(t, spec.InitContainers, 1)
	sidecar := spec.InitContainers[0]
	assert.Equal(t, vllmPreloadSidecarName, sidecar.Name)
	assert.Equal(t, "vllm-image", sidecar.Image)
	assert.Equal(t, []string{"/bin/sh", "-c", "preload"}, sidecar.Command)
	require.NotNil(t, sidecar.RestartPolicy)
	assert.Equal(t, corev1.ContainerRestartPolicyAlways, *sidecar.RestartPolicy)
	assert.Equal(t, resource.MustParse("2"), sidecar.Resources.Limits[gpu])
	assert.Equal(t, int32(pkgmodel.VLLMPreloadHealthPort), sidecar.StartupProbe.HTTPGet.Port.IntVal)
	assert.Contains(t, sidecar.Env, corev1.EnvVar{Name: "HF_TOKEN", Value: "x"})
	assert.Contains(t, sidecar.Env, corev1.EnvVar{Name: "NVIDIA_DRIVER_CAPABILITIES", Value: "compute,utility"})
	assert.NotContains(t, sidecar.Env, corev1.EnvVar{Name: "NVIDIA_VISIBLE_DEVICES", Value: "all"})
	assert.Contains(t, sidecar.VolumeMounts, corev1.VolumeMount{Name: "weights", MountPath: "/workspace/weights"})
	assert.Contains(t, sidecar.VolumeMounts, corev1.VolumeMount{Name: vllmPreloadVolumeName, MountPath: pkgmodel.VLLMPreloadSocketDir})

	main := spec.Containers[0]
	_, hasGPURequest := main.Resources.Requests[gpu]
	_, hasGPULimit := main.Resources.Limits[gpu]
	assert.False(t, hasGPURequest, "the sidecar holds the GPU request")
	assert.False(t, hasGPULimit, "the sidecar holds the GPU limit")
	assert.Equal(t, resource.MustParse("1Gi"), main.Resources.Requests[corev1.ResourceMemory])
	assert.Contains(t, main.Env, corev1.EnvVar{Name: "NVIDIA_VISIBLE_DEVICES", Value: "all"})
	assert.Contains(t, main.Env, corev1.EnvVar{Name: "NVIDIA_DRIVER_CAPABILITIES", Value: "compute,utility"})
	assert.Contains(t, main.VolumeMounts, corev1.VolumeMount{Name: vllmPreloadVolumeName, MountPath: pkgmodel.VLLMPreloadSocketDir})
	require.Len(t, spec.Volumes, 1)
	assert.NotNil(t, spec.Volumes[0].EmptyDir)
}

func TestConfigureVLLMPreloadSidecarSkipsWithoutCommand(t *testing.T) {
	spec := &corev1.PodSpec{Containers: []corev1.Container{{Name: "ws"}}}
	gctx := &builder.WorkspaceGeneratorContext{Workspace: testutil.MockWorkspaceWithPresetVLLM.DeepCopy()}
	require.NoError(t, ConfigureVLLMPreloadSidecar(&vllmPreloadResolvedConfig{})(gctx, spec))
	assert.Empty(t, spec.InitContainers)
}

func TestGenerateInferencePodSpecVLLMPreload(t *testing.T) {
	testutil.RegisterTestModel()
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	t.Setenv("PRESET_REGISTRY_NAME", "test-registry")
	oldNAP := featuregates.FeatureGates[featuregates.FeatureFlagDisableNodeAutoProvisioning]
	t.Cleanup(func() {
		featuregates.FeatureGates[featuregates.FeatureFlagDisableNodeAutoProvisioning] = oldNAP
	})
	featuregates.FeatureGates[featuregates.FeatureFlagDisableNodeAutoProvisioning] = true

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "target", Labels: map[string]string{
			"pool":                         "inference",
			corev1.LabelArchStable:         "amd64",
			corev1.LabelInstanceTypeStable: "Standard_NC6s_v3",
			nvidia.LabelGPUProduct:         "NVIDIA-V100",
			nvidia.LabelGPUCount:           "1",
			nvidia.LabelGPUMemory:          "16384",
		}},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	ws := testutil.MockWorkspaceWithPresetVLLM.DeepCopy()
	ws.Annotations = map[string]string{v1beta1.AnnotationEnableVLLMPreload: "true"}
	ws.Resource.InstanceType = ""
	ws.Resource.LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "inference"}}
	ws.Inference.Config = ""
	ws.Inference.Adapters = nil
	ws.Status.TargetNodeCount = 1
	gctx := &builder.WorkspaceGeneratorContext{Ctx: context.Background(), Workspace: ws, Model: pkgmodel.KaitoModelRegister.MustGet("test-model"), KubeClient: c}
	config, err := getGPUConfig(gctx)
	require.NoError(t, err)
	require.NotNil(t, config)

	spec := &corev1.PodSpec{}
	preload := &vllmPreloadResolvedConfig{}
	require.NoError(t, GenerateInferencePodSpec(config, 1, "", "", "", &lmCacheResolvedConfig{}, preload)(gctx, spec))
	require.NoError(t, ConfigureVLLMPreloadSidecar(preload)(gctx, spec))

	require.Len(t, preload.command, 3)
	assert.Contains(t, preload.command[2], "--kaito-preload ")
	assert.NotContains(t, preload.command[2], "ipc_cache")
	assert.True(t, strings.Contains(spec.Containers[0].Command[2], "--load-format=ipc_cache"))

	var sidecar *corev1.Container
	for i := range spec.InitContainers {
		if spec.InitContainers[i].Name == vllmPreloadSidecarName {
			sidecar = &spec.InitContainers[i]
		}
	}
	require.NotNil(t, sidecar)
	_, sidecarGPU := sidecar.Resources.Limits[corev1.ResourceName("nvidia.com/gpu")]
	_, mainGPU := spec.Containers[0].Resources.Limits[corev1.ResourceName("nvidia.com/gpu")]
	assert.True(t, sidecarGPU)
	assert.False(t, mainGPU)
}
