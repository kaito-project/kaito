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

package manifests

import (
	"encoding/json"
	"strings"
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestGenerateInferencePoolOCIRepository(t *testing.T) {
	workspace := test.MockWorkspaceWithPreset
	repo := GenerateInferencePoolOCIRepository(workspace)

	assert.Equal(t, utils.InferencePoolName(workspace.Name), repo.Name)
	assert.Equal(t, workspace.Namespace, repo.Namespace)
	assert.Len(t, repo.OwnerReferences, 1)
	owner := repo.OwnerReferences[0]
	assert.Equal(t, kaitov1beta1.GroupVersion.String(), owner.APIVersion)
	assert.Equal(t, "Workspace", owner.Kind)
	assert.Equal(t, workspace.Name, owner.Name)
	assert.True(t, *owner.Controller)

	assert.Equal(t, consts.InferencePoolChartURL, repo.Spec.URL)
	if assert.NotNil(t, repo.Spec.Reference) {
		assert.Equal(t, consts.InferencePoolChartVersion, repo.Spec.Reference.Tag)
	}
}

func TestGenerateInferencePoolHelmRelease(t *testing.T) {
	base := test.MockWorkspaceWithPreset.DeepCopy()
	base.Name = "test-workspace"
	base.Namespace = "kaito"

	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		isStatefulSet bool
		expected      map[string]any
	}{
		{
			name:          "deployment inference pool helm values",
			workspace:     base.DeepCopy(),
			isStatefulSet: false,
			expected: map[string]any{
				"inferenceExtension": map[string]any{
					"image": map[string]any{
						"hub":        consts.GatewayAPIInferenceExtensionImageRepository,
						"tag":        consts.InferencePoolChartVersion,
						"pullPolicy": string(corev1.PullIfNotPresent),
					},
					"pluginsConfigFile": "plugins-v2.yaml",
				},
				"inferencePool": map[string]any{
					"targetPortNumber": float64(consts.PortInferenceServer),
					"modelServers": map[string]any{
						"matchLabels": map[string]any{
							kaitov1beta1.LabelWorkspaceName: base.Name,
						},
					},
				},
			},
		},
		{
			name:          "statefulset inference pool helm values",
			workspace:     base.DeepCopy(),
			isStatefulSet: true,
			expected: map[string]any{
				"inferenceExtension": map[string]any{
					"image": map[string]any{
						"hub":        consts.GatewayAPIInferenceExtensionImageRepository,
						"tag":        consts.InferencePoolChartVersion,
						"pullPolicy": string(corev1.PullIfNotPresent),
					},
					"pluginsConfigFile": "plugins-v2.yaml",
				},
				"inferencePool": map[string]any{
					"targetPortNumber": float64(consts.PortInferenceServer),
					"modelServers": map[string]any{
						"matchLabels": map[string]any{
							kaitov1beta1.LabelWorkspaceName: base.Name,
							appsv1.PodIndexLabel:            "0",
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			helmRelease, err := GenerateInferencePoolHelmRelease(tc.workspace, tc.isStatefulSet)
			assert.NoError(t, err)
			assert.NotNil(t, helmRelease)

			assert.Equal(t, utils.InferencePoolName(base.Name), helmRelease.Name)
			assert.Equal(t, base.Namespace, helmRelease.Namespace)
			if assert.NotNil(t, helmRelease.Spec.ChartRef) {
				assert.Equal(t, helmv2.CrossNamespaceSourceReference{
					Kind:      sourcev1.OCIRepositoryKind,
					Namespace: base.Namespace,
					Name:      utils.InferencePoolName(base.Name),
				}, *helmRelease.Spec.ChartRef)
			}

			assert.NotNil(t, helmRelease.Spec.Values)
			vals := map[string]any{}
			assert.NoError(t, json.Unmarshal(helmRelease.Spec.Values.Raw, &vals))
			assert.Equal(t, tc.expected, vals)
		})
	}
}

func TestGeneratePullerContainers(t *testing.T) {
	base := test.MockWorkspaceWithPreset.DeepCopy()
	base.Name = "puller-ws"
	base.Namespace = "kaito"

	strength := func(s string) *string { return &s }

	volumeMounts := []corev1.VolumeMount{{Name: "shared", MountPath: "/mnt/shared"}}

	tests := []struct {
		name               string
		adapters           []kaitov1beta1.AdapterSpec
		volumeMounts       []corev1.VolumeMount
		expectedContainers int
		expectedEnvVars    map[string]string // name -> value
		expectedVolumes    int
		verify             func(t *testing.T, containers []corev1.Container, envVars []corev1.EnvVar, volumes []corev1.Volume)
	}{
		{
			name:               "no adapters",
			adapters:           nil,
			volumeMounts:       volumeMounts,
			expectedContainers: 0,
			expectedEnvVars:    map[string]string{},
			expectedVolumes:    0,
		},
		{
			name: "single adapter with strength and secrets",
			adapters: []kaitov1beta1.AdapterSpec{
				{
					Source: &kaitov1beta1.DataSource{
						Name:             "adapterA",
						Image:            "docker.io/library/alpine:latest",
						ImagePullSecrets: []string{"secretA", "secretB"},
					},
					Strength: strength("0.5"),
				},
			},
			volumeMounts:       volumeMounts,
			expectedContainers: 1,
			expectedEnvVars:    map[string]string{"adapterA": "0.5"},
			expectedVolumes:    1,
			verify: func(t *testing.T, containers []corev1.Container, envVars []corev1.EnvVar, volumes []corev1.Volume) {
				if assert.Len(t, containers, 1) {
					c := containers[0]
					assert.Equal(t, "puller-adapterA", c.Name)
					assert.Equal(t, "quay.io/skopeo/stable:v1.18.0-immutable", c.Image)
					if assert.Len(t, c.Args, 1) {
						assert.Contains(t, c.Args[0], "/mnt/adapter/adapterA")
					}
					// base volumeMount + secret volumeMount
					assert.GreaterOrEqual(t, len(c.VolumeMounts), 1)
					assert.Equal(t, volumeMounts[0], c.VolumeMounts[0])
					// secret volume mount appended
					assert.Equal(t, "docker-config-adapterA-inference-adapter", c.VolumeMounts[len(c.VolumeMounts)-1].Name)
					assert.Equal(t, "/root/.docker/config.d/adapterA-inference-adapter", c.VolumeMounts[len(c.VolumeMounts)-1].MountPath)
				}
				if assert.Len(t, volumes, 1) {
					v := volumes[0]
					assert.Equal(t, "docker-config-adapterA-inference-adapter", v.Name)
					if assert.NotNil(t, v.VolumeSource.Projected) {
						assert.Len(t, v.VolumeSource.Projected.Sources, 2)
					}
				}
			},
		},
		{
			name: "multiple adapters mixed",
			adapters: []kaitov1beta1.AdapterSpec{
				{
					Source: &kaitov1beta1.DataSource{
						Name:  "adapter1",
						Image: "docker.io/library/busybox:latest",
					},
					Strength: strength("0.7"),
				},
				{
					Source: &kaitov1beta1.DataSource{
						Name:  "adapter2",
						Image: "docker.io/library/alpine:3.19",
					},
				},
			},
			volumeMounts:       volumeMounts,
			expectedContainers: 2,
			expectedEnvVars:    map[string]string{"adapter1": "0.7"},
			expectedVolumes:    0,
			verify: func(t *testing.T, containers []corev1.Container, envVars []corev1.EnvVar, volumes []corev1.Volume) {
				// verify ordering & fields
				names := []string{"puller-adapter1", "puller-adapter2"}
				gotNames := []string{containers[0].Name, containers[1].Name}
				assert.Equal(t, names, gotNames)
				for _, c := range containers {
					if assert.Len(t, c.Args, 1) {
						// ensure path for corresponding adapter is inside script
						parts := strings.Split(c.Name, "-")
						adapterName := parts[len(parts)-1]
						assert.Contains(t, c.Args[0], "/mnt/adapter/"+adapterName)
					}
					assert.Equal(t, volumeMounts[0], c.VolumeMounts[0])
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := base.DeepCopy()
			if w.Inference == nil {
				w.Inference = &kaitov1beta1.InferenceSpec{}
			}
			w.Inference.Adapters = tc.adapters

			containers, envVars, volumes := GeneratePullerContainers(w, tc.volumeMounts)

			assert.Len(t, containers, tc.expectedContainers)
			assert.Len(t, volumes, tc.expectedVolumes)

			// build map for env var assertions
			envMap := make(map[string]string, len(envVars))
			for _, e := range envVars {
				envMap[e.Name] = e.Value
			}
			assert.Equal(t, tc.expectedEnvVars, envMap)

			if tc.verify != nil {
				tc.verify(t, containers, envVars, volumes)
			}
		})
	}
}
