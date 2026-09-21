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

package inferenceset

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/custommodel"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

const (
	customISConfigJSON = `{
		"architectures": ["LlamaForCausalLM"],
		"hidden_size": 4096,
		"intermediate_size": 14336,
		"num_hidden_layers": 32,
		"num_attention_heads": 32,
		"num_key_value_heads": 8,
		"vocab_size": 128256,
		"torch_dtype": "bfloat16"
	}`
	customISSizeBytes = int64(16060522496)
)

func customISConfigMap(configJSON string, sizeBytes int64) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "byo-config", Namespace: "default"},
		Immutable:  ptr.To(true),
		Data: map[string]string{
			models.CustomModelConfigKey: configJSON,
			models.CustomModelSizeKey:   strconv.FormatInt(sizeBytes, 10),
		},
	}
}

func customInferenceSet() *kaitov1beta1.InferenceSet {
	return &kaitov1beta1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "is", Namespace: "default"},
		Spec: kaitov1beta1.InferenceSetSpec{
			Template: kaitov1beta1.InferenceSetTemplate{
				Inference: kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: kaitov1beta1.ModelName(plugin.PresetNameCustom),
						},
					},
					Config: "byo-config",
				},
			},
		},
	}
}

func newCustomISReconciler(objs ...client.Object) *InferenceSetReconciler {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = kaitov1beta1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&kaitov1beta1.InferenceSet{}).
		Build()
	return &InferenceSetReconciler{Client: cl}
}

func isModelCondition(t *testing.T, r *InferenceSetReconciler) *metav1.Condition {
	t.Helper()
	stored := &kaitov1beta1.InferenceSet{}
	require.NoError(t, r.Client.Get(context.Background(),
		client.ObjectKey{Name: "is", Namespace: "default"}, stored))
	return meta.FindStatusCondition(stored.Status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelConfigReady))
}

func TestISReconcileResolvedModelRecordsIdentity(t *testing.T) {
	is := customInferenceSet()
	r := newCustomISReconciler(is, customISConfigMap(customISConfigJSON, customISSizeBytes))

	require.NoError(t, r.reconcileResolvedModel(context.Background(), is))

	stored := &kaitov1beta1.InferenceSet{}
	require.NoError(t, r.Client.Get(context.Background(),
		client.ObjectKey{Name: "is", Namespace: "default"}, stored))

	require.NotNil(t, stored.Status.ResolvedModel)
	assert.Equal(t, models.ConfigDigest([]byte(customISConfigJSON)), stored.Status.ResolvedModel.ConfigSHA256)
	assert.Equal(t, customISSizeBytes, stored.Status.ResolvedModel.SizeBytes)

	cond := isModelCondition(t, r)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, custommodel.ReasonResolved, cond.Reason)

	// The in-memory object must carry the record too; the replica Workspaces are
	// built from it later in the same reconcile pass.
	require.NotNil(t, is.Status.ResolvedModel)
	assert.Equal(t, customISSizeBytes, is.Status.ResolvedModel.SizeBytes)
}

func TestISReconcileResolvedModelDetectsReplacement(t *testing.T) {
	// The InferenceSet is where identity is fixed for the whole set. If a
	// replaced ConfigMap were adopted here, new replicas would be built from
	// different weights than the replicas already serving.
	is := customInferenceSet()
	is.Status.ResolvedModel = &kaitov1beta1.ResolvedModel{
		ConfigSHA256: models.ConfigDigest([]byte(customISConfigJSON)),
		SizeBytes:    customISSizeBytes,
	}

	replacement := `{
		"architectures": ["LlamaForCausalLM"],
		"hidden_size": 8192,
		"intermediate_size": 28672,
		"num_hidden_layers": 80,
		"num_attention_heads": 64,
		"num_key_value_heads": 8,
		"vocab_size": 128256,
		"torch_dtype": "bfloat16"
	}`
	r := newCustomISReconciler(is, customISConfigMap(replacement, customISSizeBytes))

	require.Error(t, r.reconcileResolvedModel(context.Background(), is))

	cond := isModelCondition(t, r)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, custommodel.ReasonReplaced, cond.Reason)

	assert.Equal(t, models.ConfigDigest([]byte(customISConfigJSON)),
		is.Status.ResolvedModel.ConfigSHA256, "the pinned identity must survive")
}

func TestISReconcileResolvedModelAdoptsChangedSize(t *testing.T) {
	// Identical configuration with a corrected size is the same model; it is
	// adopted so the whole set converges on the new value rather than wedging.
	is := customInferenceSet()
	is.Status.ResolvedModel = &kaitov1beta1.ResolvedModel{
		ConfigSHA256: models.ConfigDigest([]byte(customISConfigJSON)),
		SizeBytes:    customISSizeBytes,
	}
	corrected := customISSizeBytes * 4
	r := newCustomISReconciler(is, customISConfigMap(customISConfigJSON, corrected))

	require.NoError(t, r.reconcileResolvedModel(context.Background(), is))

	stored := &kaitov1beta1.InferenceSet{}
	require.NoError(t, r.Client.Get(context.Background(),
		client.ObjectKey{Name: "is", Namespace: "default"}, stored))
	require.NotNil(t, stored.Status.ResolvedModel)
	assert.Equal(t, corrected, stored.Status.ResolvedModel.SizeBytes)

	cond := isModelCondition(t, r)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, custommodel.ReasonResolved, cond.Reason)
}

func TestISReconcileResolvedModelReportsInvalidConfig(t *testing.T) {
	is := customInferenceSet()
	cm := customISConfigMap(customISConfigJSON, customISSizeBytes)
	delete(cm.Data, models.CustomModelSizeKey)
	r := newCustomISReconciler(is, cm)

	require.Error(t, r.reconcileResolvedModel(context.Background(), is))

	cond := isModelCondition(t, r)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, custommodel.ReasonInvalid, cond.Reason)

	stored := &kaitov1beta1.InferenceSet{}
	require.NoError(t, r.Client.Get(context.Background(),
		client.ObjectKey{Name: "is", Namespace: "default"}, stored))
	assert.Nil(t, stored.Status.ResolvedModel)
}

func TestISReconcileResolvedModelSkipsNonCustomPresets(t *testing.T) {
	// A named preset has a fixed identity; there is nothing to resolve, and no
	// ConfigMap lookup should be attempted.
	is := customInferenceSet()
	is.Spec.Template.Inference.Preset.Name = "llama-3.1-8b-instruct"
	is.Spec.Template.Inference.Config = ""
	r := newCustomISReconciler(is)

	require.NoError(t, r.reconcileResolvedModel(context.Background(), is))
	assert.Nil(t, isModelCondition(t, r))
}
