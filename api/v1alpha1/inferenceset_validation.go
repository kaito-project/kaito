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
	"reflect"
	"strings"

	"github.com/robfig/cron/v3"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/mig"
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
		errs = errs.Also(is.validateCreate().ViaField("spec"))
	} else {
		klog.InfoS("Validate update", "inferenceset", fmt.Sprintf("%s/%s", is.Namespace, is.Name))
		old := base.(*InferenceSet)
		errs = errs.Also(
			is.validateUpdate(old).ViaField("spec"),
		)
	}
	return errs
}

func (is *InferenceSet) validateCreate() (errs *apis.FieldError) {
	// Validate replicas is non-negative
	if is.Spec.Replicas != nil && *is.Spec.Replicas < 0 {
		errs = errs.Also(apis.ErrInvalidValue(*is.Spec.Replicas, "replicas", "must be non-negative"))
	}
	errs = errs.Also(validateMaintenanceWindow(is.Spec.AutoUpgrade))
	errs = errs.Also(validateInferenceSetMIG(&is.Spec.Template.Resource).ViaField("template", "resource"))
	return errs
}

func (is *InferenceSet) validateUpdate(old *InferenceSet) (errs *apis.FieldError) {
	errs = errs.Also(validateMaintenanceWindow(is.Spec.AutoUpgrade))
	// MIG is immutable once set.
	if !reflect.DeepEqual(is.Spec.Template.Resource.MIG, old.Spec.Template.Resource.MIG) {
		errs = errs.Also(apis.ErrGeneric("field is immutable", "template", "resource", "mig"))
	}
	errs = errs.Also(validateInferenceSetMIG(&is.Spec.Template.Resource).ViaField("template", "resource"))
	return errs
}

// validateInferenceSetMIG performs admission-time validation of the MIG portion of an
// InferenceSet template. Deep model-fit and tensor-parallelism checks are delegated to
// the Workspace webhook when each child Workspace is created.
func validateInferenceSetMIG(r *InferenceSetResourceSpec) (errs *apis.FieldError) {
	if r == nil || r.MIG == nil {
		return errs
	}
	if !featuregates.FeatureGates[consts.FeatureFlagEnableMIG] {
		errs = errs.Also(apis.ErrGeneric("MIG support is not enabled, set feature gate enableMIG=true", "mig"))
		return errs
	}
	if err := mig.ValidateMIGProfile(r.MIG.Profile); err != nil {
		errs = errs.Also(apis.ErrInvalidValue(err.Error(), "mig", "profile"))
		return errs
	}
	if !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		errs = errs.Also(apis.ErrGeneric("MIG is only supported with BYO nodes (disableNodeAutoProvisioning=true)", "mig"))
		return errs
	}
	if r.InstanceType != "" {
		errs = errs.Also(apis.ErrInvalidValue("instanceType must be empty when MIG is set (BYO scenario)", "instanceType"))
	}
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
