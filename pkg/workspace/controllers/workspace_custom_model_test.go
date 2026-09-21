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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/custommodel"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

const customModelConfigJSON = `{
	"architectures": ["LlamaForCausalLM"],
	"hidden_size": 4096,
	"intermediate_size": 14336,
	"num_hidden_layers": 32,
	"num_attention_heads": 32,
	"num_key_value_heads": 8,
	"max_position_embeddings": 131072,
	"vocab_size": 128256,
	"torch_dtype": "bfloat16"
}`

func customModelScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = kaitov1alpha1.AddToScheme(s)
	_ = kaitov1beta1.AddToScheme(s)
	return s
}

func customModelWorkspace() *kaitov1beta1.Workspace {
	return &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "ws", Namespace: "default"},
		Inference: &kaitov1beta1.InferenceSpec{
			Preset: &kaitov1beta1.PresetSpec{
				PresetMeta: kaitov1beta1.PresetMeta{
					Name: kaitov1beta1.ModelName(plugin.PresetNameCustom),
				},
			},
			Config: "byo-config",
		},
	}
}

const customModelSizeBytes = int64(16060522496)

func customModelConfigMap(name, configJSON string) *corev1.ConfigMap {
	return customModelConfigMapWithSize(name, configJSON, customModelSizeBytes)
}

func customModelConfigMapWithSize(name, configJSON string, sizeBytes int64) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Immutable:  ptr.To(true),
		Data: map[string]string{
			models.CustomModelConfigKey: configJSON,
			models.CustomModelSizeKey:   strconv.FormatInt(sizeBytes, 10),
		},
	}
}

func newCustomModelReconciler(objs ...client.Object) *WorkspaceReconciler {
	c := fake.NewClientBuilder().
		WithScheme(customModelScheme()).
		WithObjects(objs...).
		WithStatusSubresource(&kaitov1beta1.Workspace{}).
		Build()
	return &WorkspaceReconciler{Client: c}
}

func TestReconcileResolvedModelRecordsDigest(t *testing.T) {
	ws := customModelWorkspace()
	r := newCustomModelReconciler(ws, customModelConfigMap("byo-config", customModelConfigJSON))

	require.NoError(t, r.reconcileResolvedModel(context.Background(), ws))

	stored := &kaitov1beta1.Workspace{}
	require.NoError(t, r.Client.Get(context.Background(),
		client.ObjectKey{Name: "ws", Namespace: "default"}, stored))

	require.NotNil(t, stored.Status.ResolvedModel)
	assert.Equal(t, models.ConfigDigest([]byte(customModelConfigJSON)), stored.Status.ResolvedModel.ConfigSHA256)

	cond := meta.FindStatusCondition(stored.Status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelConfigReady))
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)

	// The in-memory copy must carry the record forward, so the rest of the
	// reconcile works from the identity that was just pinned.
	require.NotNil(t, ws.Status.ResolvedModel)
	assert.Equal(t, stored.Status.ResolvedModel.ConfigSHA256, ws.Status.ResolvedModel.ConfigSHA256)
}

func TestReconcileResolvedModelReportsReplacedConfig(t *testing.T) {
	// A ConfigMap is immutable, but it can be deleted and recreated under the
	// same name with different contents. The deployment was already sized and
	// its runtime arguments rendered from the original model, so adopting the
	// replacement would serve different weights on capacity chosen for
	// something else. It must be reported instead.
	ws := customModelWorkspace()
	ws.Status.ResolvedModel = &kaitov1beta1.ResolvedModel{
		ConfigSHA256: models.ConfigDigest([]byte(customModelConfigJSON)),
		SizeBytes:    customModelSizeBytes,
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
	r := newCustomModelReconciler(ws, customModelConfigMap("byo-config", replacement))

	err := r.reconcileResolvedModel(context.Background(), ws)
	require.Error(t, err)

	stored := &kaitov1beta1.Workspace{}
	require.NoError(t, r.Client.Get(context.Background(),
		client.ObjectKey{Name: "ws", Namespace: "default"}, stored))

	cond := meta.FindStatusCondition(stored.Status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelConfigReady))
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, custommodel.ReasonReplaced, cond.Reason)

	// The originally pinned digest must survive; overwriting it would erase the
	// only record of what the running deployment was built from.
	assert.Equal(t, models.ConfigDigest([]byte(customModelConfigJSON)),
		ws.Status.ResolvedModel.ConfigSHA256)
}

func TestReconcileResolvedModelAcceptsUnchangedConfig(t *testing.T) {
	ws := customModelWorkspace()
	ws.Status.ResolvedModel = &kaitov1beta1.ResolvedModel{
		ConfigSHA256: models.ConfigDigest([]byte(customModelConfigJSON)),
		SizeBytes:    customModelSizeBytes,
	}
	r := newCustomModelReconciler(ws, customModelConfigMap("byo-config", customModelConfigJSON))

	assert.NoError(t, r.reconcileResolvedModel(context.Background(), ws))
}

func TestReconcileResolvedModelReportsInvalidConfig(t *testing.T) {
	ws := customModelWorkspace()
	cm := customModelConfigMap("byo-config", customModelConfigJSON)
	cm.Immutable = ptr.To(false)
	r := newCustomModelReconciler(ws, cm)

	err := r.reconcileResolvedModel(context.Background(), ws)
	require.Error(t, err)

	stored := &kaitov1beta1.Workspace{}
	require.NoError(t, r.Client.Get(context.Background(),
		client.ObjectKey{Name: "ws", Namespace: "default"}, stored))

	cond := meta.FindStatusCondition(stored.Status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelConfigReady))
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, custommodel.ReasonInvalid, cond.Reason)
	assert.Nil(t, stored.Status.ResolvedModel)
}

func TestReconcileResolvedModelSkipsNonCustomPresets(t *testing.T) {
	// A normal preset has a fixed identity from its name; there is nothing to
	// resolve and no ConfigMap lookup should occur.
	ws := customModelWorkspace()
	ws.Inference.Preset.Name = kaitov1beta1.ModelName("phi-4")
	ws.Inference.Config = "runtime-settings"
	r := newCustomModelReconciler(ws)

	require.NoError(t, r.reconcileResolvedModel(context.Background(), ws))
	assert.Nil(t, ws.Status.ResolvedModel)
}

func TestReconcileResolvedModelAdoptsChangedSize(t *testing.T) {
	// The size says how much room the weights need, not which weights they are,
	// so a corrected value is a correction to the same model. It is adopted and
	// recorded rather than reported as a replacement.
	ws := customModelWorkspace()
	ws.Status.ResolvedModel = &kaitov1beta1.ResolvedModel{
		ConfigSHA256: models.ConfigDigest([]byte(customModelConfigJSON)),
		SizeBytes:    customModelSizeBytes,
	}
	corrected := customModelSizeBytes * 4
	r := newCustomModelReconciler(ws,
		customModelConfigMapWithSize("byo-config", customModelConfigJSON, corrected))

	require.NoError(t, r.reconcileResolvedModel(context.Background(), ws))

	stored := &kaitov1beta1.Workspace{}
	require.NoError(t, r.Client.Get(context.Background(),
		client.ObjectKey{Name: "ws", Namespace: "default"}, stored))

	require.NotNil(t, stored.Status.ResolvedModel)
	assert.Equal(t, corrected, stored.Status.ResolvedModel.SizeBytes,
		"status must reflect the size it resolved to now")
	assert.Equal(t, models.ConfigDigest([]byte(customModelConfigJSON)),
		stored.Status.ResolvedModel.ConfigSHA256, "the model itself is unchanged")

	cond := meta.FindStatusCondition(stored.Status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelConfigReady))
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, custommodel.ReasonResolved, cond.Reason)
}

func TestReconcileResolvedModelToleratesStatusWithoutSize(t *testing.T) {
	// A Workspace reconciled before the size was recorded has a digest but no
	// size. Treating the absent size as a mismatch would wedge a running
	// deployment on upgrade, so an unset size must simply be adopted.
	ws := customModelWorkspace()
	ws.Status.ResolvedModel = &kaitov1beta1.ResolvedModel{
		ConfigSHA256: models.ConfigDigest([]byte(customModelConfigJSON)),
	}
	r := newCustomModelReconciler(ws, customModelConfigMap("byo-config", customModelConfigJSON))

	require.NoError(t, r.reconcileResolvedModel(context.Background(), ws))
	assert.Equal(t, customModelSizeBytes, ws.Status.ResolvedModel.SizeBytes)
}

func TestReconcileResolvedModelRejectsMissingSize(t *testing.T) {
	// Without a declared size there is nothing to size the deployment from, so
	// this must surface as a configuration error rather than a default.
	ws := customModelWorkspace()
	cm := customModelConfigMap("byo-config", customModelConfigJSON)
	delete(cm.Data, models.CustomModelSizeKey)
	r := newCustomModelReconciler(ws, cm)

	err := r.reconcileResolvedModel(context.Background(), ws)
	require.Error(t, err)

	stored := &kaitov1beta1.Workspace{}
	require.NoError(t, r.Client.Get(context.Background(),
		client.ObjectKey{Name: "ws", Namespace: "default"}, stored))

	cond := meta.FindStatusCondition(stored.Status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelConfigReady))
	require.NotNil(t, cond)
	assert.Equal(t, custommodel.ReasonInvalid, cond.Reason)
	assert.Nil(t, stored.Status.ResolvedModel)
}
