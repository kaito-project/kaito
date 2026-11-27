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

package loraadapter

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/model"
)

const testKey = "kaito.sh/enable-adapter-name-prefix"

func TestValidatePrefixOptIn(t *testing.T) {
	tests := []struct {
		name    string
		ann     map[string]string
		preset  string
		runtime model.RuntimeName
		wantErr string
	}{
		{name: "absent", ann: nil, runtime: model.RuntimeNameHuggingfaceTransformers},
		{name: "false without preset", ann: map[string]string{testKey: "false"}, runtime: model.RuntimeNameHuggingfaceTransformers},
		{name: "true with vllm preset", ann: map[string]string{testKey: "true"}, preset: "p", runtime: model.RuntimeNameVLLM},
		{name: "invalid value", ann: map[string]string{testKey: "True"}, preset: "p", runtime: model.RuntimeNameVLLM, wantErr: `invalid value "True"`},
		{name: "missing preset", ann: map[string]string{testKey: "true"}, runtime: model.RuntimeNameVLLM, wantErr: "requires a preset"},
		{name: "transformers runtime", ann: map[string]string{testKey: "true"}, preset: "p", runtime: model.RuntimeNameHuggingfaceTransformers, wantErr: "requires the vLLM runtime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePrefixOptIn(tt.ann, testKey, tt.preset, tt.runtime, "metadata.annotations")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
			if !strings.Contains(err.Error(), "metadata.annotations["+testKey+"]") {
				t.Fatalf("error path missing annotation key: %v", err)
			}
		})
	}
}

func TestValidateImmutable(t *testing.T) {
	type adapter struct{ Name string }
	base := []adapter{{Name: "a"}, {Name: "b"}}
	on := map[string]string{testKey: "true"}
	tests := []struct {
		name         string
		newA         []adapter
		newAnn       map[string]string
		oldAnn       map[string]string
		wantAdapters bool
		wantAnn      bool
	}{
		{name: "unchanged", newA: base, newAnn: on, oldAnn: on},
		{name: "unrelated annotation change", newA: base, newAnn: map[string]string{testKey: "true", "x": "y"}, oldAnn: on},
		{name: "reorder", newA: []adapter{{Name: "b"}, {Name: "a"}}, newAnn: on, oldAnn: on, wantAdapters: true},
		{name: "remove", newA: base[:1], newAnn: on, oldAnn: on, wantAdapters: true},
		{name: "annotation added", newA: base, newAnn: on, oldAnn: nil, wantAnn: true},
		{name: "annotation removed", newA: base, newAnn: nil, oldAnn: on, wantAnn: true},
		{name: "false to absent", newA: base, newAnn: nil, oldAnn: map[string]string{testKey: "false"}, wantAnn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateImmutable(tt.newA, base, tt.newAnn, tt.oldAnn, testKey, "spec.adapters", "metadata.annotations", ImmutableMessage)
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			if got := strings.Contains(msg, "spec.adapters"); got != tt.wantAdapters {
				t.Errorf("adapters error = %v, want %v (err: %v)", got, tt.wantAdapters, err)
			}
			if got := strings.Contains(msg, "metadata.annotations["); got != tt.wantAnn {
				t.Errorf("annotation error = %v, want %v (err: %v)", got, tt.wantAnn, err)
			}
		})
	}
}

func TestIsInferenceSetOwned(t *testing.T) {
	const label = "inferenceset.kaito.sh/created-by"
	tests := []struct {
		name string
		obj  metav1.ObjectMeta
		want bool
	}{
		{name: "plain", obj: metav1.ObjectMeta{}},
		{name: "label", obj: metav1.ObjectMeta{Labels: map[string]string{label: "is1"}}, want: true},
		{name: "kaito owner", obj: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{APIVersion: "kaito.sh/v1alpha1", Kind: "InferenceSet"}}}, want: true},
		{name: "foreign group owner", obj: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{APIVersion: "other.io/v1", Kind: "InferenceSet"}}}},
		{name: "other kind", obj: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{APIVersion: "kaito.sh/v1beta1", Kind: "MultiRoleInference"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsInferenceSetOwned(&tt.obj, label, "kaito.sh"); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasServedModelNameOverride(t *testing.T) {
	for k, want := range map[string]bool{
		"served-model-name":   true,
		"served_model_name":   true,
		"--served-model-name": true,
		"max-model-len":       false,
		"served-model":        false,
	} {
		if got := HasServedModelNameOverride(map[string]string{k: "x"}); got != want {
			t.Errorf("%q: got %v, want %v", k, got, want)
		}
	}
}

func TestValidateVolumes(t *testing.T) {
	pvc := &corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "c"}}
	hostPath := &corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/x"}}
	emptyDir := &corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
	twoTypes := &corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "c"},
		NFS:                   &corev1.NFSVolumeSource{Server: "s", Path: "/p"},
	}
	tests := []struct {
		name     string
		adapters []VolumeSource
		old      []VolumeSource
		wantErr  string
	}{
		{name: "image source (nil volume)", adapters: []VolumeSource{{Name: "a"}}},
		{name: "pvc", adapters: []VolumeSource{{Name: "a", Volume: pvc}}},
		{name: "csi", adapters: []VolumeSource{{Name: "a", Volume: &corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{Driver: "d"}}}}},
		{name: "azureFile", adapters: []VolumeSource{{Name: "a", Volume: &corev1.VolumeSource{AzureFile: &corev1.AzureFileVolumeSource{}}}}},
		{name: "nfs", adapters: []VolumeSource{{Name: "a", Volume: &corev1.VolumeSource{NFS: &corev1.NFSVolumeSource{}}}}},
		{name: "hostPath rejected", adapters: []VolumeSource{{Name: "a", Volume: hostPath}}, wantErr: `"hostPath" is not supported`},
		{name: "emptyDir rejected", adapters: []VolumeSource{{Name: "a", Volume: emptyDir}}, wantErr: `"emptyDir" is not supported`},
		{name: "no type", adapters: []VolumeSource{{Name: "a", Volume: &corev1.VolumeSource{}}}, wantErr: "must set one of"},
		{name: "two types", adapters: []VolumeSource{{Name: "a", Volume: twoTypes}}, wantErr: "exactly one volume type"},
		{name: "error indexes second adapter", adapters: []VolumeSource{{Name: "a", Volume: pvc}, {Name: "b", Volume: hostPath}}, wantErr: "adapters[1].source.volumeSource"},
		{name: "unchanged legacy hostPath allowed on update", adapters: []VolumeSource{{Name: "a", Volume: hostPath}}, old: []VolumeSource{{Name: "a", Volume: hostPath}}},
		{name: "changed to hostPath rejected on update", adapters: []VolumeSource{{Name: "a", Volume: hostPath}}, old: []VolumeSource{{Name: "a", Volume: pvc}}, wantErr: "hostPath"},
		{name: "new hostPath adapter rejected on update", adapters: []VolumeSource{{Name: "a", Volume: hostPath}, {Name: "b", Volume: hostPath}}, old: []VolumeSource{{Name: "a", Volume: hostPath}}, wantErr: "adapters[1]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVolumes(tt.adapters, tt.old)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
