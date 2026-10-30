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

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/pkg/utils/speculativedecoding"
)

// MaxMultiRoleInferenceNameLength caps new MultiRoleInference names so the
// child InferenceSet names "<name>-<role>" fit the 63-character DNS label
// limit enforced on InferenceSet names. Prefill is the longest role type.
const MaxMultiRoleInferenceNameLength = validation.DNS1123LabelMaxLength - len("-") - len(MultiRoleInferenceRolePrefill)

func (m *MultiRoleInference) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (m *MultiRoleInference) Validate(ctx context.Context) (errs *apis.FieldError) {
	if !m.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("Skip validation for deleting multiroleinference", "multiroleinference", fmt.Sprintf("%s/%s", m.Namespace, m.Name))
		return nil
	}

	// Validate name is a valid DNS label.
	errmsgs := validation.IsDNS1123Label(m.Name)
	if len(errmsgs) > 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(errmsgs, ", "), "name"))
	}

	base := apis.GetBaseline(ctx)
	if base == nil {
		klog.InfoS("Validate creation", "multiroleinference", fmt.Sprintf("%s/%s", m.Namespace, m.Name))
		if len(m.Name) > MaxMultiRoleInferenceNameLength {
			errs = errs.Also(apis.ErrInvalidValue(fmt.Sprintf("must be no more than %d characters", MaxMultiRoleInferenceNameLength), "name"))
		}
		errs = errs.Also(m.validateCreate().ViaField("spec"))
	} else {
		klog.InfoS("Validate update", "multiroleinference", fmt.Sprintf("%s/%s", m.Namespace, m.Name))
		old := base.(*MultiRoleInference)
		errs = errs.Also(m.validateUpdate(old).ViaField("spec"))
	}
	// Speculative-decoding opt-in is validated against top-level metadata
	// (annotation propagates to child InferenceSets -> Workspaces via the
	// MRI controller). Invoke outside the spec wrapper so field paths like
	// metadata.annotations[...] are reported correctly, matching the
	// Workspace validators.
	errs = errs.Also(m.validateSpeculativeDecoding())

	// InstanceType, labelSelector immutability, template inference/config,
	// propagated annotations, and other runtime rules are enforced by
	// validating the child InferenceSets this MRI would create, so all three
	// resources (MRI -> InferenceSet -> Workspace) share one rule chain.
	if ValidateMultiRoleInferenceChildren == nil {
		return errs.Also(apis.ErrGeneric("MultiRoleInference child InferenceSet validation is not configured", "spec", "roles"))
	}
	return errs.Also(ValidateMultiRoleInferenceChildren(ctx, m))
}

// ValidateMultiRoleInferenceChildren validates the child InferenceSets a
// MultiRoleInference would create, using the admission baseline in ctx (if any)
// as the previous MRI. Returned errors are rooted at the MRI. It is assigned at
// startup (see pkg/workspace/webhooks) to a controller-side implementation,
// avoiding an import cycle with the helper package that builds the children.
var ValidateMultiRoleInferenceChildren func(ctx context.Context, m *MultiRoleInference) *apis.FieldError

func (m *MultiRoleInference) validateCreate() (errs *apis.FieldError) {
	// Validate model name is not empty.
	if m.Spec.Model.Name == "" {
		errs = errs.Also(apis.ErrMissingField("model.name"))
	}

	// Validate labelSelector is not nil and not empty.
	if m.Spec.LabelSelector == nil {
		errs = errs.Also(apis.ErrMissingField("labelSelector"))
	} else if len(m.Spec.LabelSelector.MatchLabels) == 0 && len(m.Spec.LabelSelector.MatchExpressions) == 0 {
		errs = errs.Also(apis.ErrInvalidValue("labelSelector must have at least one matchLabels or matchExpressions entry", "labelSelector"))
	}

	// Validate role topology and MRI-local invariants.
	errs = errs.Also(m.validateRoles())

	return errs
}

func (m *MultiRoleInference) validateUpdate(old *MultiRoleInference) (errs *apis.FieldError) {
	// Model name is immutable.
	if m.Spec.Model.Name != old.Spec.Model.Name {
		errs = errs.Also(apis.ErrInvalidValue(
			fmt.Sprintf("model name is immutable, was %q, now %q", old.Spec.Model.Name, m.Spec.Model.Name),
			"model.name",
		))
	}

	// Validate role topology and MRI-local invariants.
	errs = errs.Also(m.validateRoles())

	return errs
}

func (m *MultiRoleInference) validateRoles() (errs *apis.FieldError) {
	// Validate exactly 2 roles.
	if len(m.Spec.Roles) != 2 {
		errs = errs.Also(apis.ErrInvalidValue(
			fmt.Sprintf("exactly 2 roles required (one prefill, one decode), got %d", len(m.Spec.Roles)),
			"roles",
		))
		return errs
	}

	hasPrefill := false
	hasDecode := false
	for i, role := range m.Spec.Roles {
		field := fmt.Sprintf("roles[%d]", i)

		// Validate role type.
		switch role.Type {
		case MultiRoleInferenceRolePrefill:
			if hasPrefill {
				errs = errs.Also(apis.ErrInvalidValue("duplicate prefill role", field+".type"))
			}
			hasPrefill = true
		case MultiRoleInferenceRoleDecode:
			if hasDecode {
				errs = errs.Also(apis.ErrInvalidValue("duplicate decode role", field+".type"))
			}
			hasDecode = true
		default:
			errs = errs.Also(apis.ErrInvalidValue(
				fmt.Sprintf("unsupported role type %q, must be prefill or decode", role.Type),
				field+".type",
			))
		}

		// Validate replicas >= 1 when specified (nil means autoscaling).
		if role.Replicas != nil && *role.Replicas < 1 {
			errs = errs.Also(apis.ErrInvalidValue(*role.Replicas, field+".replicas", "must be at least 1"))
		}
	}

	if !hasPrefill {
		errs = errs.Also(apis.ErrMissingField("roles", "missing prefill role"))
	}
	if !hasDecode {
		errs = errs.Also(apis.ErrMissingField("roles", "missing decode role"))
	}

	return errs
}

// validateSpeculativeDecoding mirrors the Workspace/InferenceSet validators
// for the kaito.sh/enable-speculative-decoding opt-in. The MRI controller
// propagates m.Annotations onto each child InferenceSet's Spec.Template.
// Metadata, which the InferenceSet controller then clones onto each child
// Workspace. Gating at MRI admission avoids surfacing a valid MRI whose
// generated workspaces would silently drop speculative decoding.
func (m *MultiRoleInference) validateSpeculativeDecoding() (errs *apis.FieldError) {
	// Preset must be set (MRI's shared model).
	presetName := m.Spec.Model.Name
	runtime := EffectiveInferenceRuntime(m.Annotations)

	status, invalidValue := speculativedecoding.ValidateOptIn(
		m.GetAnnotations(),
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
			fmt.Sprintf("metadata.annotations[%s]", AnnotationEnableSpeculativeDecoding),
		))
	case speculativedecoding.OptInMissingPreset:
		return errs.Also(apis.ErrGeneric(
			"kaito.sh/enable-speculative-decoding requires spec.model.name",
			fmt.Sprintf("metadata.annotations[%s]", AnnotationEnableSpeculativeDecoding),
		))
	case speculativedecoding.OptInRuntimeMismatch:
		return errs.Also(apis.ErrGeneric(
			fmt.Sprintf(
				"kaito.sh/enable-speculative-decoding requires the vLLM runtime; effective runtime resolves to %q (preset %q)",
				runtime, presetName,
			),
			fmt.Sprintf("metadata.annotations[%s]", AnnotationEnableSpeculativeDecoding),
		))
	}

	// Any preset is accepted: presets registered in generator.speculativeDecodingByPreset
	// get their preset-tuned config (e.g. mtp for DeepSeek R1/V3/V3.2); everything else
	// falls back to the universal ngram default at pod-spec generation time.
	//
	// Note: MRI is inherently multi-role (prefill + decode) and typically
	// single-node per role. Pipeline-parallelism guard is enforced at the
	// child Workspace layer (Resource.Count > 1).

	return errs
}
