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
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	"github.com/stretchr/testify/assert"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
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

	assert.Equal(t, "oci://registry.k8s.io/gateway-api-inference-extension/charts/inferencepool", repo.Spec.URL)
	if assert.NotNil(t, repo.Spec.Reference) {
		assert.Equal(t, "v0.5.1", repo.Spec.Reference.Tag)
	}
}

func TestGenerateInferencePoolHelmRelease(t *testing.T) {
	workspace := test.MockWorkspaceWithPreset

	// deployment mode
	hrDep, err := GenerateInferencePoolHelmRelease(workspace, false)
	assert.NoError(t, err)
	assertBaseHelmRelease(t, workspace, hrDep)
	assert.Equal(t, "OCIRepository", hrDep.Spec.ChartRef.Kind)
	assert.Equal(t, utils.InferencePoolName(workspace.Name), hrDep.Spec.ChartRef.Name)

	// statefulset mode (currently no difference, but keep for future divergence)
	hrSts, err := GenerateInferencePoolHelmRelease(workspace, true)
	assert.NoError(t, err)
	assertBaseHelmRelease(t, workspace, hrSts)
}

func assertBaseHelmRelease(t *testing.T, w *kaitov1beta1.Workspace, hr *helmv2.HelmRelease) {
	assert.Equal(t, utils.InferencePoolName(w.Name), hr.Name)
	assert.Equal(t, w.Namespace, hr.Namespace)
	assert.Len(t, hr.OwnerReferences, 1)
	owner := hr.OwnerReferences[0]
	assert.Equal(t, kaitov1beta1.GroupVersion.String(), owner.APIVersion)
	assert.Equal(t, "Workspace", owner.Kind)
	assert.Equal(t, w.Name, owner.Name)
	assert.True(t, *owner.Controller)
	if assert.NotNil(t, hr.Spec.ChartRef) {
		assert.Equal(t, "OCIRepository", hr.Spec.ChartRef.Kind)
		assert.Equal(t, w.Namespace, hr.Spec.ChartRef.Namespace)
	}
	assert.NotNil(t, hr.Spec.Values)
}
