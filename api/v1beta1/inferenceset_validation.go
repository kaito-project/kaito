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
	return errs
}

func (is *InferenceSet) validateCreate() (errs *apis.FieldError) {
	if is.Spec.Replicas != nil && *is.Spec.Replicas < 0 {
		errs = errs.Also(apis.ErrInvalidValue(*is.Spec.Replicas, "replicas", "must be non-negative"))
	}
	errs = errs.Also(validateInferenceSetMaintenanceWindow(is.Spec.AutoUpgrade))
	return errs
}

func (is *InferenceSet) validateUpdate(_ *InferenceSet) (errs *apis.FieldError) {
	errs = errs.Also(validateInferenceSetMaintenanceWindow(is.Spec.AutoUpgrade))
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
