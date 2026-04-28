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
)

func (m *MultiRoleInference) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (m *MultiRoleInference) Validate(ctx context.Context) (errs *apis.FieldError) {
	errmsgs := validation.IsDNS1123Label(m.Name)
	if len(errmsgs) > 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(errmsgs, ", "), "name"))
	}
	base := apis.GetBaseline(ctx)
	if base == nil {
		klog.InfoS("Validate creation", "multiroleinference", fmt.Sprintf("%s/%s", m.Namespace, m.Name))
		errs = errs.Also(m.validateCreate().ViaField("spec"))
	} else {
		klog.InfoS("Validate update", "multiroleinference", fmt.Sprintf("%s/%s", m.Namespace, m.Name))
		old := base.(*MultiRoleInference)
		errs = errs.Also(m.validateUpdate(old).ViaField("spec"))
	}
	return errs
}

func (m *MultiRoleInference) validateCreate() (errs *apis.FieldError) {
	// Validate exactly 2 roles
	if len(m.Spec.Roles) != 2 {
		errs = errs.Also(apis.ErrInvalidValue(len(m.Spec.Roles), "roles", "exactly 2 roles required (one prefill and one decode)"))
	}

	// Validate role types: one prefill and one decode
	var hasPrefill, hasDecode bool
	for i, role := range m.Spec.Roles {
		switch role.Type {
		case MultiRoleInferenceRolePrefill:
			hasPrefill = true
		case MultiRoleInferenceRoleDecode:
			hasDecode = true
		default:
			errs = errs.Also(apis.ErrInvalidValue(role.Type, fmt.Sprintf("roles[%d].type", i), "must be prefill or decode"))
		}
		// Validate replicas >= 1
		if role.Replicas < 1 {
			errs = errs.Also(apis.ErrInvalidValue(role.Replicas, fmt.Sprintf("roles[%d].replicas", i), "must be at least 1"))
		}
		// Validate instanceType is not empty
		if role.InstanceType == "" {
			errs = errs.Also(apis.ErrMissingField(fmt.Sprintf("roles[%d].instanceType", i)))
		}
	}
	if len(m.Spec.Roles) == 2 && (!hasPrefill || !hasDecode) {
		errs = errs.Also(apis.ErrInvalidValue("missing prefill or decode role", "roles", "exactly one prefill and one decode role required"))
	}

	// Validate model name is not empty
	if m.Spec.Model.Name == "" {
		errs = errs.Also(apis.ErrMissingField("model.name"))
	}

	// Validate labelSelector is not nil
	if m.Spec.LabelSelector == nil {
		errs = errs.Also(apis.ErrMissingField("labelSelector"))
	}

	return errs
}

func (m *MultiRoleInference) validateUpdate(_ *MultiRoleInference) (errs *apis.FieldError) {
	// Run the same validations as create to prevent invalid updates.
	errs = errs.Also(m.validateCreate())
	return errs
}
