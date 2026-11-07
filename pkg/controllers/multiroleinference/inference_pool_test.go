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

package multiroleinference

import (
	"context"
	"encoding/json"
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/internal/inferencepool"
)

func TestReconcileInferencePoolSources(t *testing.T) {
	mirror := inferencepool.Sources{
		ChartURL:           "oci://mirror.example.com/charts/llm-d-router-gateway",
		ChartVersion:       "v0.9.1",
		ChartProvider:      "aws",
		EPPImageRegistry:   "mirror.example.com/llm-d",
		EPPImageRepository: "mirrored-epp",
		EPPImageTag:        "v0.9.1-mirror",
	}
	for name, tc := range map[string]struct {
		sources      inferencepool.Sources
		wantProvider string
	}{
		"defaults":  {sources: inferencepool.DefaultSources(), wantProvider: ""},
		"overrides": {sources: mirror, wantProvider: "aws"},
	} {
		sources := tc.sources
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, kaitov1alpha1.AddToScheme(scheme))
			require.NoError(t, sourcev1.AddToScheme(scheme))
			require.NoError(t, helmv2.AddToScheme(scheme))

			mri := &kaitov1alpha1.MultiRoleInference{
				ObjectMeta: metav1.ObjectMeta{Name: "mri-test", Namespace: "default"},
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mri).Build()
			r := NewMultiRoleInferenceReconciler(cl, scheme, logr.Discard(), nil, true, sources)

			require.NoError(t, r.reconcileInferencePool(ctx, mri))

			key := client.ObjectKey{Name: inferencePoolName(mri.Name), Namespace: mri.Namespace}
			ociRepo := &sourcev1.OCIRepository{}
			require.NoError(t, cl.Get(ctx, key, ociRepo))
			assert.Equal(t, sources.ChartURL, ociRepo.Spec.URL)
			assert.Equal(t, tc.wantProvider, ociRepo.Spec.Provider)
			require.NotNil(t, ociRepo.Spec.Reference)
			assert.Equal(t, sources.ChartVersion, ociRepo.Spec.Reference.Tag)

			helmRelease := &helmv2.HelmRelease{}
			require.NoError(t, cl.Get(ctx, key, helmRelease))
			values := map[string]any{}
			require.NoError(t, json.Unmarshal(helmRelease.Spec.Values.Raw, &values))
			image := values["router"].(map[string]any)["epp"].(map[string]any)["image"].(map[string]any)
			assert.Equal(t, sources.EPPImageRegistry, image["registry"])
			assert.Equal(t, sources.EPPImageRepository, image["repository"])
			assert.Equal(t, sources.EPPImageTag, image["tag"])
		})
	}
}
