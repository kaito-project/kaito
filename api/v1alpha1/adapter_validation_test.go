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

package v1alpha1

import (
	"context"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/model"
)

const (
	childImmutableMsg = "adapters are immutable on a Workspace created by an InferenceSet"
	isImmutableMsg    = "adapters are immutable; delete and recreate the InferenceSet"
	hostPathMsg       = `adapter volume type "hostPath" is not supported`
)

var (
	pvcVolume      = &v1.VolumeSource{PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{ClaimName: "adapters"}}
	hostPathVolume = &v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/adapters"}}
	prefixOn       = map[string]string{AnnotationEnableAdapterNamePrefix: "true"}
)

func wsAdapter(name string, vs *v1.VolumeSource) AdapterSpec {
	if vs != nil {
		return AdapterSpec{Source: &DataSource{Name: name, Volume: vs}}
	}
	return AdapterSpec{Source: &DataSource{Name: name, Image: "registry.example.com/" + name + ":1"}}
}

func isAdapter(name string, vs *v1.VolumeSource) kaitov1beta1.AdapterSpec {
	if vs != nil {
		return kaitov1beta1.AdapterSpec{Source: &kaitov1beta1.DataSource{Name: name, Volume: vs}}
	}
	return kaitov1beta1.AdapterSpec{Source: &kaitov1beta1.DataSource{Name: name, Image: "registry.example.com/" + name + ":1"}}
}

func prefixWorkspace(preset string, annotations map[string]string, adapters ...AdapterSpec) *Workspace {
	return &Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "ws", Namespace: "kaito", Annotations: annotations},
		Resource:   ResourceSpec{InstanceType: "Standard_NC24ads_A100_v4", Count: pointerToInt(1)},
		Inference: &InferenceSpec{
			Preset:   &PresetSpec{PresetMeta: PresetMeta{Name: ModelName(preset)}},
			Adapters: adapters,
		},
	}
}

func adapterTestCtx(t *testing.T) context.Context {
	t.Helper()
	RegisterValidationTestModels()
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(&v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "override", Namespace: "kaito"},
		Data:       map[string]string{"inference_config.yaml": "vllm:\n  served-model-name: other\n"},
	}).Build()
	return testCtx(withClient(cl))
}

func assertContains(t *testing.T, err *apis.FieldError, want string, expect bool) {
	t.Helper()
	got := ""
	if err != nil {
		got = err.Error()
	}
	if strings.Contains(got, want) != expect {
		t.Fatalf("error containing %q = %v, want %v; err: %v", want, !expect, expect, err)
	}
}

func TestWorkspaceAdapterRules(t *testing.T) {
	ctx := adapterTestCtx(t)
	update := func(old *Workspace) context.Context { return apis.WithinUpdate(ctx, old) }

	t.Run("prefix annotation invalid value rejected", func(t *testing.T) {
		ws := prefixWorkspace("test-validation", map[string]string{AnnotationEnableAdapterNamePrefix: "yes"})
		assertContains(t, ws.Validate(ctx), `invalid value "yes"`, true)
	})
	t.Run("prefix annotation with transformers runtime rejected", func(t *testing.T) {
		ws := prefixWorkspace("test-validation", map[string]string{
			AnnotationEnableAdapterNamePrefix: "true",
			AnnotationWorkspaceRuntime:        string(model.RuntimeNameHuggingfaceTransformers),
		})
		assertContains(t, ws.Validate(ctx), "requires the vLLM runtime", true)
	})
	t.Run("prefix annotation with vLLM preset accepted", func(t *testing.T) {
		ws := prefixWorkspace("test-validation", prefixOn, wsAdapter("a", nil))
		assertContains(t, ws.Validate(ctx), AnnotationEnableAdapterNamePrefix, false)
	})
	t.Run("invalid adapter name rejected on create", func(t *testing.T) {
		ws := prefixWorkspace("test-validation", nil, wsAdapter("Bad_Name", nil))
		assertContains(t, ws.Validate(ctx), "source.name", true)
	})
	t.Run("nil adapter source rejected on create without panic", func(t *testing.T) {
		ws := prefixWorkspace("test-validation", nil, AdapterSpec{})
		assertContains(t, ws.Validate(ctx), "missing field(s): inference.Source", true)
	})
	t.Run("hostPath adapter rejected on create", func(t *testing.T) {
		ws := prefixWorkspace("test-validation", nil, wsAdapter("a", hostPathVolume))
		assertContains(t, ws.Validate(ctx), hostPathMsg, true)
	})
	t.Run("unchanged hostPath adapter allowed on update", func(t *testing.T) {
		old := prefixWorkspace("test-validation", nil, wsAdapter("a", hostPathVolume))
		ws := prefixWorkspace("test-validation", map[string]string{"x": "y"}, wsAdapter("a", hostPathVolume))
		assertContains(t, ws.Validate(update(old)), hostPathMsg, false)
	})
	t.Run("served-model-name override rejected with prefix", func(t *testing.T) {
		ws := prefixWorkspace("test-validation", prefixOn, wsAdapter("a", nil))
		ws.Inference.Config = "override"
		assertContains(t, ws.Validate(ctx), "must not set served-model-name", true)
	})
	t.Run("served-model-name override allowed without prefix", func(t *testing.T) {
		ws := prefixWorkspace("test-validation", nil, wsAdapter("a", nil))
		ws.Inference.Config = "override"
		assertContains(t, ws.Validate(ctx), "must not set served-model-name", false)
	})
	t.Run("InferenceSet child adapter change rejected even if label is removed", func(t *testing.T) {
		old := prefixWorkspace("test-validation", nil, wsAdapter("a", nil))
		old.Labels = map[string]string{kaitov1beta1.WorkspaceCreatedByInferenceSetLabel: "is1"}
		ws := prefixWorkspace("test-validation", nil, wsAdapter("b", nil))
		assertContains(t, ws.Validate(update(old)), childImmutableMsg, true)
	})
	t.Run("standalone adapter change allowed", func(t *testing.T) {
		old := prefixWorkspace("test-validation", nil, wsAdapter("a", nil))
		ws := prefixWorkspace("test-validation", nil, wsAdapter("b", nil))
		assertContains(t, ws.Validate(update(old)), childImmutableMsg, false)
	})
}

func TestInferenceSetAdapterRules(t *testing.T) {
	newIS := func(annotations map[string]string, replicas int32, adapters ...kaitov1beta1.AdapterSpec) *InferenceSet {
		is := &InferenceSet{ObjectMeta: metav1.ObjectMeta{Name: "is1", Namespace: "kaito"}}
		is.Spec.Replicas = &replicas
		is.Spec.Template.Annotations = annotations
		is.Spec.Template.Inference.Preset = &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: "test-validation"}}
		is.Spec.Template.Inference.Adapters = adapters
		return is
	}
	ctx := testCtx()

	t.Run("invalid adapter name rejected on create", func(t *testing.T) {
		err := newIS(nil, 1, isAdapter("Bad_Name", nil)).Validate(ctx)
		assertContains(t, err, "spec.template.inference.adapters", true)
	})
	t.Run("hostPath adapter rejected on create", func(t *testing.T) {
		err := newIS(nil, 1, isAdapter("a", hostPathVolume)).Validate(ctx)
		assertContains(t, err, hostPathMsg, true)
		assertContains(t, err, "spec.template.inference.adapters[0].source.volumeSource", true)
	})
	t.Run("prefix annotation with transformers runtime on the InferenceSet rejected", func(t *testing.T) {
		is := newIS(prefixOn, 1, isAdapter("a", nil))
		is.Annotations = map[string]string{AnnotationWorkspaceRuntime: string(model.RuntimeNameHuggingfaceTransformers)}
		err := is.Validate(ctx)
		assertContains(t, err, "requires the vLLM runtime", true)
		assertContains(t, err, "spec.template.metadata.annotations", true)
	})

	base := newIS(prefixOn, 1, isAdapter("a", nil), isAdapter("b", pvcVolume))
	tests := []struct {
		name   string
		is     *InferenceSet
		reject bool
	}{
		{name: "adapter added", is: newIS(prefixOn, 1, isAdapter("a", nil), isAdapter("b", pvcVolume), isAdapter("c", nil)), reject: true},
		{name: "adapter removed", is: newIS(prefixOn, 1, isAdapter("a", nil)), reject: true},
		{name: "adapters reordered", is: newIS(prefixOn, 1, isAdapter("b", pvcVolume), isAdapter("a", nil)), reject: true},
		{name: "annotation removed", is: newIS(nil, 1, isAdapter("a", nil), isAdapter("b", pvcVolume)), reject: true},
		{name: "replicas changed", is: newIS(prefixOn, 4, isAdapter("a", nil), isAdapter("b", pvcVolume))},
		{
			name: "deletion in progress",
			is: func() *InferenceSet {
				is := newIS(nil, 1)
				now := metav1.Now()
				is.DeletionTimestamp = &now
				return is
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertContains(t, tt.is.Validate(apis.WithinUpdate(ctx, base)), isImmutableMsg, tt.reject)
		})
	}
}
