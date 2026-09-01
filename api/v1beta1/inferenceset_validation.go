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
	"strings"

	"github.com/robfig/cron/v3"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
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
		klog.V(2).InfoS("Validate creation", "inferenceset", fmt.Sprintf("%s/%s", is.Namespace, is.Name))
		errs = errs.Also(is.validateCreate().ViaField("spec"))
	} else {
		klog.V(2).InfoS("Validate update", "inferenceset", fmt.Sprintf("%s/%s", is.Namespace, is.Name))
		old := base.(*InferenceSet)
		errs = errs.Also(
			is.validateUpdate(old).ViaField("spec"),
		)
	}
	// Speculative-decoding opt-in is reported using paths rooted at
	// spec.template.metadata.annotations, so it must be invoked outside the
	// spec wrapper to avoid a doubled spec.spec.template... path.
	errs = errs.Also(is.validateSpeculativeDecoding())
	if ValidateInferenceSetWorkspace != nil {
		errs = errs.Also(ValidateInferenceSetWorkspace(ctx, is).ViaField("spec", "template"))
	}
	return errs
}

// ValidateInferenceSetWorkspace validates the child Workspace an InferenceSet
// would create. It is assigned at startup (see pkg/workspace/webhooks) to a
// controller-side implementation, avoiding an import cycle with the util
// package that builds the Workspace.
var ValidateInferenceSetWorkspace func(ctx context.Context, is *InferenceSet) *apis.FieldError

func (is *InferenceSet) validateCreate() (errs *apis.FieldError) {
	if is.Spec.Replicas != nil && *is.Spec.Replicas < 0 {
		errs = errs.Also(apis.ErrInvalidValue(*is.Spec.Replicas, "replicas", "must be non-negative"))
	}
	errs = errs.Also(is.validateInstanceType().ViaField("template"))
	errs = errs.Also(validateInferenceSetMaintenanceWindow(is.Spec.AutoUpgrade))
	return errs
}

func (is *InferenceSet) validateUpdate(old *InferenceSet) (errs *apis.FieldError) {
	errs = errs.Also(is.validateInstanceType().ViaField("template"))
	errs = errs.Also(validateInferenceSetMaintenanceWindow(is.Spec.AutoUpgrade))
	// Partition config is immutable once set.
	if !apiequality.Semantic.DeepEqual(is.Spec.Template.Resource.Partition, old.Spec.Template.Resource.Partition) {
		errs = errs.Also(apis.ErrGeneric("field is immutable", "template", "resource", "partition"))
	}
	return errs
}

// validateSpeculativeDecoding mirrors the Workspace validator for the
// kaito.sh/enable-speculative-decoding opt-in. The InferenceSet controller
// clones Spec.Template.Annotations onto each child Workspace, so gating at
// admission avoids surfacing a valid InferenceSet whose replicas would then
// silently drop speculative decoding.
func (is *InferenceSet) validateSpeculativeDecoding() (errs *apis.FieldError) {
	val, present := is.Spec.Template.Annotations[AnnotationEnableSpeculativeDecoding]
	if !present {
		return nil
	}
	switch val {
	case "false":
		return nil
	case "true":
		// fall through to preset/runtime checks
	default:
		return errs.Also(apis.ErrGeneric(
			fmt.Sprintf(
				"kaito.sh/enable-speculative-decoding must be \"true\" or \"false\"; got %q",
				val,
			),
			fmt.Sprintf("spec.template.metadata.annotations[%s]", AnnotationEnableSpeculativeDecoding),
		))
	}

	inf := is.Spec.Template.Inference
	if inf.Preset == nil || inf.Preset.Name == "" {
		return errs.Also(apis.ErrGeneric(
			"kaito.sh/enable-speculative-decoding requires a preset inference; "+
				"remove the annotation or set spec.template.inference.preset.name",
			fmt.Sprintf("spec.template.metadata.annotations[%s]", AnnotationEnableSpeculativeDecoding),
		))
	}

	// Any preset is accepted: presets registered in generator.speculativeDecodingByPreset
	// get their preset-tuned config (e.g. mtp for DeepSeek R1/V3); everything else
	// falls back to the universal ngram default at pod-spec generation time.
	presetName := string(inf.Preset.Name)

	// Runtime must be vLLM. Uses the same helper the runtime layer uses,
	// so a workload that would resolve to HuggingFace at pod-spec generation
	// time (e.g. because the FeatureFlagVLLM feature gate is disabled) is
	// rejected here instead of being admitted only to be rejected later at
	// the Workspace layer.
	if runtime := EffectiveInferenceRuntime(is.Spec.Template.Annotations); runtime != model.RuntimeNameVLLM {
		return errs.Also(apis.ErrGeneric(
			fmt.Sprintf(
				"kaito.sh/enable-speculative-decoding requires the vLLM runtime; "+
					"effective runtime resolves to %q (preset %q)",
				runtime, presetName,
			),
			fmt.Sprintf("spec.template.metadata.annotations[%s]", AnnotationEnableSpeculativeDecoding),
		))
	}
	return errs
}

func validateInferenceSetMaintenanceWindow(autoUpgrade *AutoUpgradePolicy) (errs *apis.FieldError) {
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
func (is *InferenceSet) validateInstanceType() (errs *apis.FieldError) {
	instanceType := is.Spec.Template.Resource.InstanceType
	switch consts.ActiveNodeProvisioner {
	case consts.NodeProvisionerBYO:
		// BYO mode: instanceType must be empty.
		if instanceType != "" {
			errs = errs.Also(apis.ErrInvalidValue(instanceType, "resource.instanceType",
				"instanceType must be empty when nodeProvisioner is byo"))
		}
	case consts.NodeProvisionerKarpenter, consts.NodeProvisionerAzureGPU:
		// Auto-provisioning modes: instanceType is required.
		if instanceType == "" {
			errs = errs.Also(apis.ErrMissingField("resource.instanceType"))
		}
	default:
		// Unknown or unset provisioner: no validation (backward compat).
	}
	return errs
}
