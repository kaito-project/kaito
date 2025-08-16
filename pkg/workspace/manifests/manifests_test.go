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
