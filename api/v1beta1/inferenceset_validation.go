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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"
)

func (is *InferenceSet) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (is *InferenceSet) Validate(ctx context.Context) (errs *apis.FieldError) {
	if !is.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("Skip validation for deleting inferenceset", "inferenceset", fmt.Sprintf("%s/%s", is.Namespace, is.Name))
		return nil
	}

	errmsgs := validation.IsDNS1123Label(is.Name)
	if len(errmsgs) > 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(errmsgs, ", "), "name"))
	}
	if apis.GetBaseline(ctx) == nil {
		klog.V(2).InfoS("Validate creation", "inferenceset", fmt.Sprintf("%s/%s", is.Namespace, is.Name))
		errs = errs.Also(is.validateCreate().ViaField("spec"))
	} else {
		klog.V(2).InfoS("Validate update", "inferenceset", fmt.Sprintf("%s/%s", is.Namespace, is.Name))
		errs = errs.Also(validateInferenceSetMaintenanceWindow(is.Spec.AutoUpgrade).ViaField("spec"))
	}
	// Template-level rules (instanceType, partition, annotations, inference
	// immutability, ...) are enforced by validating the child Workspace this
	// InferenceSet would create, so both resources share one rule set. Fail
	// closed if that validator is not wired rather than skip those rules.
	if ValidateInferenceSetWorkspace == nil {
		return errs.Also(apis.ErrGeneric("InferenceSet child Workspace validation is not configured", "spec", "template"))
	}
	return errs.Also(ValidateInferenceSetWorkspace(ctx, is))
}

// ValidateInferenceSetWorkspace validates the child Workspace an InferenceSet
// would create, using the admission baseline in ctx (if any) as the previous
// InferenceSet. Returned errors are rooted at the InferenceSet. It is assigned
// at startup (see pkg/workspace/webhooks) to a controller-side implementation,
// avoiding an import cycle with the util package that builds the Workspace.
var ValidateInferenceSetWorkspace func(ctx context.Context, is *InferenceSet) *apis.FieldError

func (is *InferenceSet) validateCreate() (errs *apis.FieldError) {
	if is.Spec.Replicas != nil && *is.Spec.Replicas < 0 {
		errs = errs.Also(apis.ErrInvalidValue(*is.Spec.Replicas, "replicas", "must be non-negative"))
	}
	errs = errs.Also(validateInferenceSetMaintenanceWindow(is.Spec.AutoUpgrade))
	errs = errs.Also(validateInferenceSetSelectorNotEmpty(is.Spec.Selector))
	errs = errs.Also(validateInferenceSetSelectorMatchLabels(is.Spec.Selector, is.Spec.Template.Labels))
	return errs
}

// validateInferenceSetSelectorNotEmpty rejects an unset or empty spec.labelSelector.
// It is enforced on create only so existing InferenceSets with an empty
// (immutable) selector remain updatable. Paths are relative to spec.
func validateInferenceSetSelectorNotEmpty(selector *metav1.LabelSelector) *apis.FieldError {
	if selector == nil {
		return apis.ErrMissingField("labelSelector")
	}
	if IsEmptyLabelSelector(selector) {
		return apis.ErrGeneric("labelSelector must not be empty", "labelSelector")
	}
	return nil
}

// validateInferenceSetSelectorMatchLabels rejects template.metadata.labels that
// conflict with spec.labelSelector.matchLabels, which are copied onto every child
// Workspace. matchExpressions are not checked: MultiRoleInference may generate
// selectors whose expressions describe nodes rather than the template labels.
// Paths are relative to spec.
func validateInferenceSetSelectorMatchLabels(selector *metav1.LabelSelector, templateLabels map[string]string) (errs *apis.FieldError) {
	if selector == nil {
		return nil
	}
	for k, v := range selector.MatchLabels {
		if tv, ok := templateLabels[k]; ok && tv != v {
			errs = errs.Also(apis.ErrInvalidValue(tv, fmt.Sprintf("template.metadata.labels[%s]", k),
				fmt.Sprintf("must equal labelSelector.matchLabels[%s] %q", k, v)))
		}
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
