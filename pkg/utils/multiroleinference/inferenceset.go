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

package multiroleinference

import (
	"context"
	"fmt"
	"maps"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

// NewInferenceSetForMultiRoleInference builds the child InferenceSet object for
// the given MRI role. It is the single source of truth for child-InferenceSet
// construction: the MRI controller reconciles from it, and the MRI admission
// webhook validates the exact InferenceSets it would generate.
func NewInferenceSetForMultiRoleInference(mri *kaitov1alpha1.MultiRoleInference, role kaitov1alpha1.MultiRoleInferenceRoleSpec) *kaitov1beta1.InferenceSet {
	roleStr := string(role.Type)
	// Template labels carry the same selector keys so child Workspaces match.
	// With node auto-provisioning, the InferenceSet controller also inherits
	// this selector as the Workspace node selector.
	selector := mri.Spec.LabelSelector.DeepCopy()
	if selector == nil {
		selector = &metav1.LabelSelector{}
	}
	if selector.MatchLabels == nil {
		selector.MatchLabels = make(map[string]string)
	}
	selector.MatchLabels[kaitov1alpha1.LabelMultiRoleInferenceParent] = mri.Name
	selector.MatchLabels[kaitov1alpha1.LabelInferenceRole] = roleStr

	templateLabels := make(map[string]string)
	if mri.Spec.LabelSelector != nil && mri.Spec.LabelSelector.MatchLabels != nil {
		for k, v := range mri.Spec.LabelSelector.MatchLabels {
			templateLabels[k] = v
		}
	}
	templateLabels[kaitov1alpha1.LabelMultiRoleInferenceParent] = mri.Name
	templateLabels[kaitov1alpha1.LabelInferenceRole] = roleStr

	is := &kaitov1beta1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", mri.Name, role.Type),
			Namespace: mri.Namespace,
			Labels: map[string]string{
				kaitov1alpha1.LabelMultiRoleInferenceParent: mri.Name,
				kaitov1alpha1.LabelInferenceRole:            roleStr,
			},
		},
		Spec: kaitov1beta1.InferenceSetSpec{
			Selector: selector,
			Template: kaitov1beta1.InferenceSetTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      templateLabels,
					Annotations: maps.Clone(mri.Annotations),
				},
				Resource: kaitov1beta1.InferenceSetResourceSpec{
					InstanceType: role.InstanceType,
				},
				Inference: kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: kaitov1beta1.ModelName(mri.Spec.Model.Name),
						},
						PresetOptions: kaitov1beta1.PresetOptions{
							ModelAccessSecret: mri.Spec.Model.ModelAccessSecret,
						},
					},
				},
			},
		},
	}
	if role.Replicas != nil {
		is.Spec.Replicas = role.Replicas
	}
	if role.RuntimeConfig != "" {
		is.Spec.Template.Inference.Config = role.RuntimeConfig
	}
	return is
}

// ValidateInferenceSetsForMultiRoleInference validates the child InferenceSets
// an MRI would create, using the same admission rules the InferenceSet webhook
// applies (and, transitively, the Workspace rules projected from each child
// InferenceSet). Returned errors are rooted at the MRI.
func ValidateInferenceSetsForMultiRoleInference(ctx context.Context, mri *kaitov1alpha1.MultiRoleInference) (errs *apis.FieldError) {
	var old *kaitov1alpha1.MultiRoleInference
	if base, ok := apis.GetBaseline(ctx).(*kaitov1alpha1.MultiRoleInference); ok {
		old = base
	}

	for i, role := range mri.Spec.Roles {
		isCtx := ctx
		if oldRole, ok := findRoleByType(old, role.Type); ok {
			isCtx = apis.WithinUpdate(ctx, NewInferenceSetForMultiRoleInference(old, oldRole))
		}
		if err := NewInferenceSetForMultiRoleInference(mri, role).Validate(isCtx); err != nil {
			errs = errs.Also(remapChildInferenceSetErrors(err, i))
		}
	}

	return errs
}

func remapChildInferenceSetErrors(err *apis.FieldError, roleIndex int) (errs *apis.FieldError) {
	if err == nil {
		return nil
	}
	for _, wrapped := range err.WrappedErrors() {
		mappedPaths := make([]string, 0, len(wrapped.Paths))
		for _, path := range wrapped.Paths {
			mappedPaths = append(mappedPaths, remapChildInferenceSetPath(path, roleIndex))
		}
		if len(mappedPaths) == 0 {
			mappedPaths = []string{fmt.Sprintf("spec.roles[%d]", roleIndex)}
		}
		errs = errs.Also(&apis.FieldError{
			Message: wrapped.Message,
			Paths:   mappedPaths,
			Level:   wrapped.Level,
			Details: wrapped.Details,
		})
	}
	return errs
}

func remapChildInferenceSetPath(path string, roleIndex int) string {
	// Workspace create checks prefix some resource and metadata errors with
	// spec, while other checks return those paths directly.
	if strings.HasPrefix(path, "spec.template.spec.") {
		path = "spec.template." + strings.TrimPrefix(path, "spec.template.spec.")
	}
	switch {
	case path == "":
		return fmt.Sprintf("spec.roles[%d]", roleIndex)
	case path == "spec.labelSelector" || strings.HasPrefix(path, "spec.labelSelector."):
		return path
	case path == "spec.template.resource.labelSelector" || strings.HasPrefix(path, "spec.template.resource.labelSelector."):
		return strings.Replace(path, "spec.template.resource.labelSelector", "spec.labelSelector", 1)
	case strings.HasPrefix(path, "spec.template.metadata.annotations["):
		return strings.Replace(path, "spec.template.", "", 1)
	case strings.HasPrefix(path, "spec.template.resource.instanceType"):
		return strings.Replace(path, "spec.template.resource.instanceType", fmt.Sprintf("spec.roles[%d].instanceType", roleIndex), 1)
	case path == "spec.template.config" || strings.HasPrefix(path, "spec.template.config."):
		return strings.Replace(path, "spec.template.config", fmt.Sprintf("spec.roles[%d].runtimeConfig", roleIndex), 1)
	case strings.HasPrefix(path, "spec.template.inference.config"):
		return strings.Replace(path, "spec.template.inference.config", fmt.Sprintf("spec.roles[%d].runtimeConfig", roleIndex), 1)
	case path == "spec.template.inference_config.yaml",
		path == "spec.template.inference_config.yaml in ConfigMap",
		path == "spec.template.max-model-len":
		// ConfigMap content errors point to the MRI's configuration reference.
		return fmt.Sprintf("spec.roles[%d].runtimeConfig", roleIndex)
	case strings.HasPrefix(path, "spec.template.inference.preset.presetOptions.modelAccessSecret"):
		return strings.Replace(path, "spec.template.inference.preset.presetOptions.modelAccessSecret", "spec.model.modelAccessSecret", 1)
	case path == "spec.template.inference.preset.name":
		return "spec.model.name"
	case strings.HasPrefix(path, "spec.template.inference.presetName"):
		return strings.Replace(path, "spec.template.inference.presetName", "spec.model.name", 1)
	case path == "spec.template.inference.preset":
		// Workspace immutability compares the whole preset, including its secret.
		return "spec.model"
	case strings.HasPrefix(path, "spec.template.inference.preset"):
		return strings.Replace(path, "spec.template.inference.preset", "spec.model.name", 1)
	case strings.HasPrefix(path, "spec.replicas"):
		return strings.Replace(path, "spec.replicas", fmt.Sprintf("spec.roles[%d].replicas", roleIndex), 1)
	default:
		return fmt.Sprintf("spec.roles[%d]", roleIndex)
	}
}

func findRoleByType(mri *kaitov1alpha1.MultiRoleInference, roleType kaitov1alpha1.MultiRoleInferenceRoleType) (kaitov1alpha1.MultiRoleInferenceRoleSpec, bool) {
	if mri == nil {
		return kaitov1alpha1.MultiRoleInferenceRoleSpec{}, false
	}
	for _, role := range mri.Spec.Roles {
		if role.Type == roleType {
			return role, true
		}
	}
	return kaitov1alpha1.MultiRoleInferenceRoleSpec{}, false
}
