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
	"fmt"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/env"
	"github.com/kaito-project/kaito/api/internal/loraadapter"
	"github.com/kaito-project/kaito/pkg/model"
)

// ValidateAdapters applies the per-adapter field rules and the duplicate-name
// check. It is exported for the v1alpha1 InferenceSet, whose template embeds
// v1beta1 adapters.
func ValidateAdapters(adapters []AdapterSpec) (errs *apis.FieldError) {
	for _, adapter := range adapters {
		errs = errs.Also(adapter.validateCreateorUpdate())
	}
	if len(adapters) > 0 {
		errs = errs.Also(validateDuplicateName(adapters, map[string]bool{}))
	}
	return errs
}

// adapterVolumeSources returns the volume view of adapters used by the
// adapter volume allowlist.
func adapterVolumeSources(adapters []AdapterSpec) []loraadapter.VolumeSource {
	out := make([]loraadapter.VolumeSource, 0, len(adapters))
	for _, a := range adapters {
		if a.Source == nil {
			continue
		}
		out = append(out, loraadapter.VolumeSource{Name: a.Source.Name, Volume: a.Source.Volume})
	}
	return out
}

func inferenceAdapters(w *Workspace) []AdapterSpec {
	if w == nil || w.Inference == nil {
		return nil
	}
	return w.Inference.Adapters
}

func (w *Workspace) adapterNamePrefixEnabled() bool {
	return w.GetAnnotations()[AnnotationEnableAdapterNamePrefix] == "true" && len(inferenceAdapters(w)) > 0
}

func (w *Workspace) validateAdapterNamePrefix() *apis.FieldError {
	presetName := ""
	if w.Inference != nil && w.Inference.Preset != nil {
		presetName = string(w.Inference.Preset.Name)
	}
	return loraadapter.ValidatePrefixOptIn(w.GetAnnotations(), AnnotationEnableAdapterNamePrefix, presetName,
		GetWorkspaceRuntimeName(w), "metadata.annotations")
}

// validateAdapterVolumes enforces the adapter volume allowlist. old is nil on
// create; on update, adapters whose volume is unchanged are not re-checked.
func (w *Workspace) validateAdapterVolumes(old *Workspace) *apis.FieldError {
	var oldSources []loraadapter.VolumeSource
	if old != nil {
		oldSources = adapterVolumeSources(inferenceAdapters(old))
	}
	return loraadapter.ValidateVolumes(adapterVolumeSources(inferenceAdapters(w)), oldSources).ViaField("inference")
}

// validateChildAdapterImmutability keeps a Workspace created by an
// InferenceSet from being edited directly to change what the InferenceSet
// forbids changing. Ownership is read from old so that removing the label in
// the same update does not bypass the check.
func (w *Workspace) validateChildAdapterImmutability(old *Workspace) *apis.FieldError {
	if !loraadapter.IsInferenceSetOwned(old, WorkspaceCreatedByInferenceSetLabel, GroupVersion.Group) {
		return nil
	}
	return loraadapter.ValidateImmutable(inferenceAdapters(w), inferenceAdapters(old), w.GetAnnotations(), old.GetAnnotations(),
		AnnotationEnableAdapterNamePrefix, "inference.adapters", "metadata.annotations", loraadapter.ChildImmutableMessage)
}

// validateAdapterNamePrefixConfig rejects an inference config that overrides
// served-model-name while adapter names are prefixed with it. Pass a nil old on
// create. On update it runs only when the adapters, the annotation, or the
// config name changed, so a later ConfigMap edit does not block unrelated
// updates. A ConfigMap that is missing or unparsable is reported by
// validateInferenceConfig, not here.
func (w *Workspace) validateAdapterNamePrefixConfig(ctx context.Context, old *Workspace) *apis.FieldError {
	if !w.adapterNamePrefixEnabled() || w.Inference.Config == "" || GetWorkspaceRuntimeName(w) != model.RuntimeNameVLLM {
		return nil
	}
	if old != nil && old.Inference != nil && w.Inference.Config == old.Inference.Config &&
		w.GetAnnotations()[AnnotationEnableAdapterNamePrefix] == old.GetAnnotations()[AnnotationEnableAdapterNamePrefix] &&
		apiequality.Semantic.DeepEqual(inferenceAdapters(w), inferenceAdapters(old)) {
		return nil
	}
	kubeClient, err := env.FromContext(ctx).RequireClient()
	if err != nil {
		return apis.ErrGeneric(err.Error())
	}
	var cm corev1.ConfigMap
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: w.Inference.Config, Namespace: w.Namespace}, &cm); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return apis.ErrGeneric(fmt.Sprintf("Failed to get ConfigMap '%s' in namespace '%s': %v", w.Inference.Config, w.Namespace, err), "inference.config")
	}
	var cfg InferenceConfig
	parseErr := yaml.Unmarshal([]byte(cm.Data["inference_config.yaml"]), &cfg)
	if parseErr == nil && loraadapter.HasServedModelNameOverride(cfg.VLLM) {
		return apis.ErrGeneric(
			fmt.Sprintf("inference config must not set served-model-name when %s is \"true\"; adapter IDs are prefixed with the served model name", AnnotationEnableAdapterNamePrefix),
			"inference.config",
		)
	}
	return nil
}
