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

// Package loraadapter holds LoRA adapter admission rules shared by the
// v1alpha1 and v1beta1 Workspace and InferenceSet validators.
package loraadapter

import (
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/pkg/model"
)

const (
	// ImmutableMessage is reported when an InferenceSet update changes its
	// adapters or the adapter-name-prefix annotation.
	ImmutableMessage = "adapters are immutable; delete and recreate the InferenceSet"
	// ChildImmutableMessage is reported when a Workspace created by an
	// InferenceSet is edited directly to change the same values.
	ChildImmutableMessage = "adapters are immutable on a Workspace created by an InferenceSet; delete and recreate the InferenceSet"

	inferenceSetKind = "InferenceSet"

	prefixEnabled  = "true"
	prefixDisabled = "false"
)

// allowedVolumeTypes are the VolumeSource members (by JSON name) accepted as an
// adapter source. Node-local types such as hostPath are deliberately excluded.
var allowedVolumeTypes = []string{"persistentVolumeClaim", "csi", "azureFile", "nfs"}

// ValidatePrefixOptIn validates the adapter-name-prefix annotation value and
// that "true" is only used with a vLLM preset. path is the annotations field
// path, e.g. "metadata.annotations".
func ValidatePrefixOptIn(annotations map[string]string, annotationKey, presetName string, runtime model.RuntimeName, path string) *apis.FieldError {
	val, present := annotations[annotationKey]
	if !present || val == prefixDisabled {
		return nil
	}
	fieldPath := fmt.Sprintf("%s[%s]", path, annotationKey)
	if val != prefixEnabled {
		return apis.ErrInvalidValue(
			fmt.Sprintf("annotation %s has invalid value %q; expected %q or %q", annotationKey, val, prefixEnabled, prefixDisabled),
			fieldPath,
		)
	}
	if presetName == "" {
		return apis.ErrGeneric(
			fmt.Sprintf("%s requires a preset inference; remove the annotation or set a preset name", annotationKey),
			fieldPath,
		)
	}
	if runtime != model.RuntimeNameVLLM {
		return apis.ErrGeneric(
			fmt.Sprintf("%s requires the vLLM runtime; the effective runtime is %q", annotationKey, runtime),
			fieldPath,
		)
	}
	return nil
}

// ValidateImmutable rejects any change to the adapter list (order-sensitive)
// or to the presence or value of annotationKey.
func ValidateImmutable[T any](newAdapters, oldAdapters []T, newAnnotations, oldAnnotations map[string]string,
	annotationKey, adaptersPath, annotationsPath, msg string) (errs *apis.FieldError) {
	if !apiequality.Semantic.DeepEqual(newAdapters, oldAdapters) {
		errs = errs.Also(apis.ErrGeneric(msg, adaptersPath))
	}
	newVal, newOK := newAnnotations[annotationKey]
	oldVal, oldOK := oldAnnotations[annotationKey]
	if newOK != oldOK || newVal != oldVal {
		errs = errs.Also(apis.ErrGeneric(msg, fmt.Sprintf("%s[%s]", annotationsPath, annotationKey)))
	}
	return errs
}

// IsInferenceSetOwned reports whether obj was created by a KAITO InferenceSet,
// either through an owner reference or the created-by label.
func IsInferenceSetOwned(obj metav1.Object, createdByLabel, group string) bool {
	if obj.GetLabels()[createdByLabel] != "" {
		return true
	}
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Kind != inferenceSetKind {
			continue
		}
		if gv, err := schema.ParseGroupVersion(ref.APIVersion); err == nil && gv.Group == group {
			return true
		}
	}
	return false
}

// HasServedModelNameOverride reports whether a vLLM inference config sets
// served-model-name, in either dash or underscore spelling.
func HasServedModelNameOverride(vllmConfig map[string]string) bool {
	for k := range vllmConfig {
		if strings.ReplaceAll(strings.TrimLeft(k, "-"), "_", "-") == "served-model-name" {
			return true
		}
	}
	return false
}

// VolumeSource is the version-independent view of an adapter source used by
// ValidateVolumes.
type VolumeSource struct {
	Name   string
	Volume *corev1.VolumeSource
}

// ValidateVolumes rejects adapter volumes whose type is not allowlisted.
// Adapters whose volume is unchanged from the same-named adapter in old are
// skipped so that existing objects stay updatable. Pass a nil old on create.
// Error paths are "adapters[<i>].source.volumeSource".
func ValidateVolumes(adapters, old []VolumeSource) (errs *apis.FieldError) {
	oldByName := make(map[string]*corev1.VolumeSource, len(old))
	for _, o := range old {
		oldByName[o.Name] = o.Volume
	}
	for i, a := range adapters {
		if a.Volume == nil {
			continue
		}
		if prev, ok := oldByName[a.Name]; ok && apiequality.Semantic.DeepEqual(prev, a.Volume) {
			continue
		}
		if msg := volumeTypeError(a.Volume); msg != "" {
			errs = errs.Also(apis.ErrGeneric(msg, fmt.Sprintf("adapters[%d].source.volumeSource", i)))
		}
	}
	return errs
}

func volumeTypeError(v *corev1.VolumeSource) string {
	var set []string
	rv := reflect.ValueOf(*v)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		if f := rv.Field(i); f.Kind() == reflect.Ptr && !f.IsNil() {
			set = append(set, strings.Split(rt.Field(i).Tag.Get("json"), ",")[0])
		}
	}
	allowed := strings.Join(allowedVolumeTypes, ", ")
	switch {
	case len(set) == 0:
		return fmt.Sprintf("adapter volume must set one of: %s", allowed)
	case len(set) > 1:
		return fmt.Sprintf("adapter volume must set exactly one volume type, got: %s", strings.Join(set, ", "))
	}
	for _, t := range allowedVolumeTypes {
		if set[0] == t {
			return ""
		}
	}
	return fmt.Sprintf("adapter volume type %q is not supported; use one of: %s", set[0], allowed)
}
