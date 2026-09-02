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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

func TestReconcileInferencePoolUsesConfiguredSourceAndImage(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kaitov1alpha1.AddToScheme(scheme))
	require.NoError(t, helmv2.AddToScheme(scheme))
	require.NoError(t, sourcev1.AddToScheme(scheme))

	mri := &kaitov1alpha1.MultiRoleInference{}
	mri.Name = "mri-test"
	mri.Namespace = "default"
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mri).Build()
	r := NewMultiRoleInferenceReconcilerWithConfig(
		cl,
		scheme,
		logr.Discard(),
		record.NewFakeRecorder(10),
		true,
		InferencePoolConfig{
			ChartURL:         "oci://registry.example/charts/inference-pool",
			ChartTag:         "v1.2.3",
			EPPImageRegistry: "registry.example",
			EPPImageRepo:     "mirrors/epp",
			EPPImageTag:      "v4.5.6",
		},
	)

	require.NoError(t, r.reconcileInferencePool(context.Background(), mri))

	ociRepo := &sourcev1.OCIRepository{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: "mri-test-inferencepool", Namespace: "default"}, ociRepo))
	assert.Equal(t, "oci://registry.example/charts/inference-pool", ociRepo.Spec.URL)
	require.NotNil(t, ociRepo.Spec.Reference)
	assert.Equal(t, "v1.2.3", ociRepo.Spec.Reference.Tag)

	helmRelease := &helmv2.HelmRelease{}
	require.NoError(t, cl.Get(context.Background(), client.ObjectKey{Name: "mri-test-inferencepool", Namespace: "default"}, helmRelease))
	var values map[string]any
	require.NoError(t, json.Unmarshal(helmRelease.Spec.Values.Raw, &values))
	eppImage := values["router"].(map[string]any)["epp"].(map[string]any)["image"].(map[string]any)
	assert.Equal(t, "registry.example", eppImage["registry"])
	assert.Equal(t, "mirrors/epp", eppImage["repository"])
	assert.Equal(t, "v4.5.6", eppImage["tag"])
}

func TestNewMultiRoleInferenceReconcilerWithConfigDefaultsEmptyFields(t *testing.T) {
	r := NewMultiRoleInferenceReconcilerWithConfig(nil, nil, logr.Discard(), nil, false, InferencePoolConfig{})
	assert.Equal(t, DefaultInferencePoolConfig(), r.InferencePoolConfig)
}
