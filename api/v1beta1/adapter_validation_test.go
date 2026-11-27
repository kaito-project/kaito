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

package v1beta1

import (
	"context"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/pkg/model"
)

const (
	prefixOn          = "true"
	childImmutableMsg = "adapters are immutable on a Workspace created by an InferenceSet"
	isImmutableMsg    = "adapters are immutable; delete and recreate the InferenceSet"
	servedNameMsg     = "must not set served-model-name"
)

func imageAdapter(name string) AdapterSpec {
	return AdapterSpec{Source: &DataSource{Name: name, Image: "registry.example.com/" + name + ":1"}}
}

func volumeAdapter(name string, vs *v1.VolumeSource) AdapterSpec {
	return AdapterSpec{Source: &DataSource{Name: name, Volume: vs}}
}

var (
	pvcVolume      = &v1.VolumeSource{PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{ClaimName: "adapters"}}
	hostPathVolume = &v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/adapters"}}
)

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

func inferenceConfigMap(name, vllmYAML string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kaito"},
		Data:       map[string]string{"inference_config.yaml": "vllm:\n" + vllmYAML},
	}
}

func adapterTestCtx(t *testing.T) context.Context {
	t.Helper()
	RegisterValidationTestModels()
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(
		inferenceConfigMap("override-dash", "  served-model-name: other\n"),
		inferenceConfigMap("override-underscore", "  served_model_name: other\n"),
		inferenceConfigMap("plain", "  max-model-len: \"2048\"\n"),
	).Build()
	return testCtx(withClient(cl))
}

func errString(err *apis.FieldError) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func assertContains(t *testing.T, err *apis.FieldError, want string, expect bool) {
	t.Helper()
	if got := strings.Contains(errString(err), want); got != expect {
		t.Fatalf("error containing %q = %v, want %v; err: %v", want, got, expect, err)
	}
}

func TestWorkspaceValidateAdapterNamePrefixAnnotation(t *testing.T) {
	ctx := adapterTestCtx(t)
	key := AnnotationEnableAdapterNamePrefix
	tests := []struct {
		name    string
		ws      *Workspace
		wantErr string
	}{
		{name: "true with vLLM preset", ws: prefixWorkspace("test-vllm-only", map[string]string{key: "true"}, imageAdapter("a"))},
		{name: "false", ws: prefixWorkspace("test-vllm-only", map[string]string{key: "false"})},
		{name: "invalid value", ws: prefixWorkspace("test-vllm-only", map[string]string{key: "yes"}), wantErr: `invalid value "yes"`},
		{
			name:    "transformers runtime",
			ws:      prefixWorkspace("test-validation", map[string]string{key: "true", AnnotationWorkspaceRuntime: string(model.RuntimeNameHuggingfaceTransformers)}),
			wantErr: "requires the vLLM runtime",
		},
		{
			name: "no preset",
			ws: &Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "ws", Namespace: "kaito", Annotations: map[string]string{key: "true"}},
				Inference:  &InferenceSpec{Template: &v1.PodTemplateSpec{}},
			},
			wantErr: "requires a preset inference",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ws.Validate(ctx)
			if tt.wantErr == "" {
				assertContains(t, err, key, false)
				return
			}
			assertContains(t, err, tt.wantErr, true)
			assertContains(t, err, "metadata.annotations["+key+"]", true)
		})
	}

	t.Run("invalid value rejected on update", func(t *testing.T) {
		old := prefixWorkspace("test-vllm-only", nil)
		ws := prefixWorkspace("test-vllm-only", map[string]string{key: "1"})
		assertContains(t, ws.Validate(apis.WithinUpdate(ctx, old)), `invalid value "1"`, true)
	})
}

func TestWorkspaceValidateAdapterCreateRules(t *testing.T) {
	ctx := adapterTestCtx(t)
	t.Run("invalid adapter name rejected on create", func(t *testing.T) {
		ws := prefixWorkspace("test-vllm-only", nil, imageAdapter("Bad_Name"))
		assertContains(t, ws.Validate(ctx), "adapters.source.name", true)
	})
	t.Run("nil adapter source rejected on create without panic", func(t *testing.T) {
		ws := prefixWorkspace("test-vllm-only", nil, AdapterSpec{}, imageAdapter("a"))
		assertContains(t, ws.Validate(ctx), "missing field(s): inference.Source", true)
	})
	t.Run("nil adapter source rejected on update without panic", func(t *testing.T) {
		old := prefixWorkspace("test-vllm-only", nil, imageAdapter("a"))
		ws := prefixWorkspace("test-vllm-only", nil, AdapterSpec{}, imageAdapter("a"))
		assertContains(t, ws.Validate(apis.WithinUpdate(ctx, old)), "missing field(s): inference.Source", true)
	})
}

func TestWorkspaceValidateAdapterVolumeAllowlist(t *testing.T) {
	ctx := adapterTestCtx(t)
	const notSupported = `adapter volume type "hostPath" is not supported`
	t.Run("hostPath rejected on create", func(t *testing.T) {
		ws := prefixWorkspace("test-vllm-only", nil, volumeAdapter("a", hostPathVolume))
		err := ws.Validate(ctx)
		assertContains(t, err, notSupported, true)
		assertContains(t, err, "inference.adapters[0].source.volumeSource", true)
	})
	t.Run("pvc accepted on create", func(t *testing.T) {
		ws := prefixWorkspace("test-vllm-only", nil, volumeAdapter("a", pvcVolume))
		assertContains(t, ws.Validate(ctx), "adapter volume", false)
	})
	t.Run("existing hostPath adapter stays updatable", func(t *testing.T) {
		old := prefixWorkspace("test-vllm-only", nil, volumeAdapter("a", hostPathVolume))
		ws := prefixWorkspace("test-vllm-only", map[string]string{"unrelated": "x"}, volumeAdapter("a", hostPathVolume))
		assertContains(t, ws.Validate(apis.WithinUpdate(ctx, old)), notSupported, false)
	})
	t.Run("new hostPath adapter rejected on update", func(t *testing.T) {
		old := prefixWorkspace("test-vllm-only", nil, volumeAdapter("a", pvcVolume))
		ws := prefixWorkspace("test-vllm-only", nil, volumeAdapter("a", pvcVolume), volumeAdapter("b", hostPathVolume))
		assertContains(t, ws.Validate(apis.WithinUpdate(ctx, old)), notSupported, true)
	})
}

func TestWorkspaceValidateInferenceSetChildAdapterUpdate(t *testing.T) {
	ctx := adapterTestCtx(t)
	childMeta := func(ws *Workspace) *Workspace {
		ws.Labels = map[string]string{WorkspaceCreatedByInferenceSetLabel: "is1"}
		return ws
	}
	tests := []struct {
		name   string
		old    *Workspace
		new    *Workspace
		reject bool
	}{
		{
			name:   "child adapter added",
			old:    childMeta(prefixWorkspace("test-vllm-only", nil, imageAdapter("a"))),
			new:    childMeta(prefixWorkspace("test-vllm-only", nil, imageAdapter("a"), imageAdapter("b"))),
			reject: true,
		},
		{
			name:   "child label removed in the same update",
			old:    childMeta(prefixWorkspace("test-vllm-only", nil, imageAdapter("a"))),
			new:    prefixWorkspace("test-vllm-only", nil, imageAdapter("b")),
			reject: true,
		},
		{
			name: "child owned by InferenceSet owner reference only",
			old: func() *Workspace {
				ws := prefixWorkspace("test-vllm-only", nil, imageAdapter("a"))
				ws.OwnerReferences = []metav1.OwnerReference{{APIVersion: GroupVersion.String(), Kind: "InferenceSet", Name: "is1"}}
				return ws
			}(),
			new:    prefixWorkspace("test-vllm-only", nil),
			reject: true,
		},
		{
			name:   "child annotation toggled",
			old:    childMeta(prefixWorkspace("test-vllm-only", nil, imageAdapter("a"))),
			new:    childMeta(prefixWorkspace("test-vllm-only", map[string]string{AnnotationEnableAdapterNamePrefix: prefixOn}, imageAdapter("a"))),
			reject: true,
		},
		{
			name: "child metadata update by controller",
			old:  childMeta(prefixWorkspace("test-vllm-only", map[string]string{AnnotationEnableAdapterNamePrefix: prefixOn}, imageAdapter("a"))),
			new: func() *Workspace {
				ws := childMeta(prefixWorkspace("test-vllm-only", map[string]string{AnnotationEnableAdapterNamePrefix: prefixOn, AnnotationEnableSpeculativeDecoding: "true"}, imageAdapter("a")))
				ws.Labels["team"] = "x"
				return ws
			}(),
		},
		{
			name: "standalone adapter update",
			old:  prefixWorkspace("test-vllm-only", nil, imageAdapter("a")),
			new:  prefixWorkspace("test-vllm-only", nil, imageAdapter("b")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertContains(t, tt.new.Validate(apis.WithinUpdate(ctx, tt.old)), childImmutableMsg, tt.reject)
		})
	}
}

func TestWorkspaceValidateAdapterNamePrefixConfig(t *testing.T) {
	ctx := adapterTestCtx(t)
	on := map[string]string{AnnotationEnableAdapterNamePrefix: prefixOn}
	withConfig := func(ws *Workspace, cm string) *Workspace {
		ws.Inference.Config = cm
		return ws
	}
	t.Run("create rejects served-model-name override", func(t *testing.T) {
		for _, cm := range []string{"override-dash", "override-underscore"} {
			ws := withConfig(prefixWorkspace("test-vllm-only", on, imageAdapter("a")), cm)
			assertContains(t, ws.Validate(ctx), servedNameMsg, true)
		}
	})
	t.Run("create allows override without the annotation", func(t *testing.T) {
		ws := withConfig(prefixWorkspace("test-vllm-only", nil, imageAdapter("a")), "override-dash")
		assertContains(t, ws.Validate(ctx), servedNameMsg, false)
	})
	t.Run("create allows override without adapters", func(t *testing.T) {
		ws := withConfig(prefixWorkspace("test-vllm-only", on), "override-dash")
		assertContains(t, ws.Validate(ctx), servedNameMsg, false)
	})
	t.Run("create allows config without the override", func(t *testing.T) {
		ws := withConfig(prefixWorkspace("test-vllm-only", on, imageAdapter("a")), "plain")
		assertContains(t, ws.Validate(ctx), servedNameMsg, false)
	})
	t.Run("update enabling the annotation rejects override", func(t *testing.T) {
		old := withConfig(prefixWorkspace("test-vllm-only", nil, imageAdapter("a")), "override-dash")
		ws := withConfig(prefixWorkspace("test-vllm-only", on, imageAdapter("a")), "override-dash")
		assertContains(t, ws.Validate(apis.WithinUpdate(ctx, old)), servedNameMsg, true)
	})
	t.Run("update switching config to an override is rejected", func(t *testing.T) {
		old := withConfig(prefixWorkspace("test-vllm-only", on, imageAdapter("a")), "plain")
		ws := withConfig(prefixWorkspace("test-vllm-only", on, imageAdapter("a")), "override-underscore")
		assertContains(t, ws.Validate(apis.WithinUpdate(ctx, old)), servedNameMsg, true)
	})
	t.Run("unrelated update is not blocked by a later ConfigMap override", func(t *testing.T) {
		old := withConfig(prefixWorkspace("test-vllm-only", on, imageAdapter("a")), "override-dash")
		ws := withConfig(prefixWorkspace("test-vllm-only", on, imageAdapter("a")), "override-dash")
		ws.Labels = map[string]string{"team": "x"}
		assertContains(t, ws.Validate(apis.WithinUpdate(ctx, old)), servedNameMsg, false)
	})
}

func TestInferenceSetValidateAdapterImmutability(t *testing.T) {
	original := ValidateInferenceSetWorkspace
	t.Cleanup(func() { ValidateInferenceSetWorkspace = original })
	ValidateInferenceSetWorkspace = func(context.Context, *InferenceSet) *apis.FieldError { return nil }

	newIS := func(annotations map[string]string, replicas int32, adapters ...AdapterSpec) *InferenceSet {
		is := &InferenceSet{ObjectMeta: metav1.ObjectMeta{Name: "is1", Namespace: "kaito"}}
		is.Spec.Replicas = &replicas
		is.Spec.Template.Annotations = annotations
		is.Spec.Template.Inference.Adapters = adapters
		return is
	}
	on := map[string]string{AnnotationEnableAdapterNamePrefix: prefixOn}
	base := newIS(on, 1, imageAdapter("a"), imageAdapter("b"))
	tests := []struct {
		name   string
		is     *InferenceSet
		reject bool
	}{
		{name: "adapter added", is: newIS(on, 1, imageAdapter("a"), imageAdapter("b"), imageAdapter("c")), reject: true},
		{name: "adapter removed", is: newIS(on, 1, imageAdapter("a")), reject: true},
		{name: "adapters reordered", is: newIS(on, 1, imageAdapter("b"), imageAdapter("a")), reject: true},
		{name: "adapter source changed", is: newIS(on, 1, imageAdapter("a"), volumeAdapter("b", pvcVolume)), reject: true},
		{name: "annotation removed", is: newIS(nil, 1, imageAdapter("a"), imageAdapter("b")), reject: true},
		{name: "annotation set to false", is: newIS(map[string]string{AnnotationEnableAdapterNamePrefix: "false"}, 1, imageAdapter("a"), imageAdapter("b")), reject: true},
		{name: "replicas changed", is: newIS(on, 3, imageAdapter("a"), imageAdapter("b"))},
		{name: "unrelated annotation added", is: newIS(map[string]string{AnnotationEnableAdapterNamePrefix: prefixOn, "x": "y"}, 1, imageAdapter("a"), imageAdapter("b"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertContains(t, tt.is.Validate(apis.WithinUpdate(context.Background(), base)), isImmutableMsg, tt.reject)
		})
	}

	t.Run("hostPath adapter rejected on create", func(t *testing.T) {
		is := newIS(nil, 1, volumeAdapter("a", hostPathVolume))
		err := is.Validate(context.Background())
		assertContains(t, err, `adapter volume type "hostPath" is not supported`, true)
		assertContains(t, err, "spec.template.inference.adapters[0].source.volumeSource", true)
	})
}
