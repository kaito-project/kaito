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
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/api/env"
	"github.com/kaito-project/kaito/api/internal/loraadapter"
	"github.com/kaito-project/kaito/api/internal/speculativedecoding"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/model"
)

func (is *InferenceSet) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (is *InferenceSet) Validate(ctx context.Context) (errs *apis.FieldError) {
	errmsgs := validation.IsDNS1123Label(is.Name)
	if len(errmsgs) > 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(errmsgs, ", "), "name"))
	}
	base := apis.GetBaseline(ctx)
	if base == nil {
		klog.InfoS("Validate creation", "inferenceset", fmt.Sprintf("%s/%s", is.Namespace, is.Name))
		errs = errs.Also(is.validateCreate(ctx).ViaField("spec"))
		errs = errs.Also(
			kaitov1beta1.ValidateAdapters(is.Spec.Template.Inference.Adapters).ViaField("spec", "template", "inference"),
			is.validateAdapterVolumes(),
		)
	} else {
		klog.InfoS("Validate update", "inferenceset", fmt.Sprintf("%s/%s", is.Namespace, is.Name))
		old := base.(*InferenceSet)
		errs = errs.Also(
			is.validateUpdate(ctx, old).ViaField("spec"),
		)
		if old.DeletionTimestamp.IsZero() && is.DeletionTimestamp.IsZero() {
			errs = errs.Also(is.validateAdapterImmutability(old))
		}
	}
	errs = errs.Also(is.validateSpeculativeDecoding())
	errs = errs.Also(is.validateAdapterNamePrefix())
	return errs
}

func (is *InferenceSet) validateCreate(ctx context.Context) (errs *apis.FieldError) {
	// Validate replicas is non-negative
	if is.Spec.Replicas != nil && *is.Spec.Replicas < 0 {
		errs = errs.Also(apis.ErrInvalidValue(*is.Spec.Replicas, "replicas", "must be non-negative"))
	}
	errs = errs.Also(is.validateInstanceType(ctx).ViaField("template"))
	errs = errs.Also(validateMaintenanceWindow(is.Spec.AutoUpgrade))
	return errs
}

func (is *InferenceSet) validateUpdate(ctx context.Context, _ *InferenceSet) (errs *apis.FieldError) {
	errs = errs.Also(is.validateInstanceType(ctx).ViaField("template"))
	errs = errs.Also(validateMaintenanceWindow(is.Spec.AutoUpgrade))
	return errs
}

// validateSpeculativeDecoding mirrors the v1beta1 InferenceSet validator for
// the kaito.sh/enable-speculative-decoding opt-in so both served versions
// enforce the same admission contract.
func (is *InferenceSet) validateSpeculativeDecoding() (errs *apis.FieldError) {
	inf := is.Spec.Template.Inference
	presetName := ""
	if inf.Preset != nil {
		presetName = string(inf.Preset.Name)
	}
	runtime := EffectiveInferenceRuntime(is.Spec.Template.Annotations)

	status, invalidValue := speculativedecoding.ValidateOptIn(
		is.Spec.Template.Annotations,
		AnnotationEnableSpeculativeDecoding,
		presetName,
		runtime,
	)
	switch status {
	case speculativedecoding.OptInDisabled:
		return nil
	case speculativedecoding.OptInInvalidValue:
		return errs.Also(apis.ErrInvalidValue(
			fmt.Sprintf("annotation %s has invalid value %q; expected \"true\" or \"false\"", AnnotationEnableSpeculativeDecoding, invalidValue),
			fmt.Sprintf("spec.template.metadata.annotations[%s]", AnnotationEnableSpeculativeDecoding),
		))
	case speculativedecoding.OptInMissingPreset:
		return errs.Also(apis.ErrGeneric(
			"kaito.sh/enable-speculative-decoding requires a preset inference; remove the annotation or set spec.template.inference.preset.name",
			fmt.Sprintf("spec.template.metadata.annotations[%s]", AnnotationEnableSpeculativeDecoding),
		))
	case speculativedecoding.OptInRuntimeMismatch:
		return errs.Also(apis.ErrGeneric(
			fmt.Sprintf(
				"kaito.sh/enable-speculative-decoding requires the vLLM runtime; effective runtime resolves to %q (preset %q)",
				runtime, presetName,
			),
			fmt.Sprintf("spec.template.metadata.annotations[%s]", AnnotationEnableSpeculativeDecoding),
		))
	}

	// Any preset is accepted: presets registered in generator.speculativeDecodingByPreset
	// get their preset-tuned config (e.g. mtp for DeepSeek R1/V3/V3.2); everything else
	// falls back to the universal ngram default at pod-spec generation time.
	return errs
}

func validateMaintenanceWindow(autoUpgrade *AutoUpgradePolicy) (errs *apis.FieldError) {
	if autoUpgrade == nil || autoUpgrade.MaintenanceWindow == nil {
		return nil
	}
	window := autoUpgrade.MaintenanceWindow
	if window.Schedule == "" {
		errs = errs.Also(apis.ErrMissingField("autoUpgrade.maintenanceWindow.schedule"))
		return errs
	}
	if _, err := cron.ParseStandard(window.Schedule); err != nil {
		errs = errs.Also(apis.ErrInvalidValue(window.Schedule, "autoUpgrade.maintenanceWindow.schedule",
			fmt.Sprintf("invalid cron expression: %v", err)))
	}
	if window.Duration != nil && window.Duration.Duration <= 0 {
		errs = errs.Also(apis.ErrInvalidValue(window.Duration.Duration.String(), "autoUpgrade.maintenanceWindow.duration",
			"must be a positive duration"))
	}
	return errs
}

// validateInstanceType ensures instanceType is set when node auto-provisioning
// is enabled, and is empty when using BYO (Bring Your Own) nodes.
func (is *InferenceSet) validateInstanceType(ctx context.Context) (errs *apis.FieldError) {
	instanceType := is.Spec.Template.Resource.InstanceType
	switch env.FromContext(ctx).NodeProvisioner {
	case env.NodeProvisionerBYO:
		// BYO mode: instanceType must be empty.
		if instanceType != "" {
			errs = errs.Also(apis.ErrInvalidValue(instanceType, "resource.instanceType",
				"instanceType must be empty when nodeProvisioner is byo"))
		}
	case env.NodeProvisionerKarpenter, env.NodeProvisionerAzureGPU:
		// Auto-provisioning modes: instanceType is required.
		if instanceType == "" {
			errs = errs.Also(apis.ErrMissingField("resource.instanceType"))
		}
	default:
		// Unknown or unset provisioner: no validation (backward compat).
	}
	return errs
}

// effectiveRuntime mirrors how the child Workspace resolves its runtime: the
// InferenceSet's own runtime annotation wins over the template's.
func (is *InferenceSet) effectiveRuntime() model.RuntimeName {
	if runtime := is.GetAnnotations()[AnnotationWorkspaceRuntime]; runtime != "" {
		return EffectiveInferenceRuntime(map[string]string{AnnotationWorkspaceRuntime: runtime})
	}
	return EffectiveInferenceRuntime(is.Spec.Template.Annotations)
}

func (is *InferenceSet) validateAdapterNamePrefix() *apis.FieldError {
	presetName := ""
	if is.Spec.Template.Inference.Preset != nil {
		presetName = string(is.Spec.Template.Inference.Preset.Name)
	}
	return loraadapter.ValidatePrefixOptIn(is.Spec.Template.Annotations, AnnotationEnableAdapterNamePrefix, presetName,
		is.effectiveRuntime(), "spec.template.metadata.annotations")
}

// validateAdapterVolumes enforces the adapter volume allowlist. It runs only on
// create because adapters are immutable afterwards.
func (is *InferenceSet) validateAdapterVolumes() *apis.FieldError {
	adapters := is.Spec.Template.Inference.Adapters
	sources := make([]loraadapter.VolumeSource, 0, len(adapters))
	for _, a := range adapters {
		if a.Source != nil {
			sources = append(sources, loraadapter.VolumeSource{Name: a.Source.Name, Volume: a.Source.Volume})
		}
	}
	return loraadapter.ValidateVolumes(sources, nil).ViaField("spec", "template", "inference")
}

// validateAdapterImmutability rejects changes to the template adapters or the
// adapter-name-prefix annotation, which would otherwise roll out per replica
// and briefly serve different adapter sets behind one endpoint.
func (is *InferenceSet) validateAdapterImmutability(old *InferenceSet) *apis.FieldError {
	return loraadapter.ValidateImmutable(
		is.Spec.Template.Inference.Adapters, old.Spec.Template.Inference.Adapters,
		is.Spec.Template.Annotations, old.Spec.Template.Annotations,
		AnnotationEnableAdapterNamePrefix,
		"spec.template.inference.adapters", "spec.template.metadata.annotations",
		loraadapter.ImmutableMessage,
	)
}
