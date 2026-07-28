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

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func (m *MultiRoleInference) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (m *MultiRoleInference) Validate(ctx context.Context) (errs *apis.FieldError) {
	// Validate name is a valid DNS label.
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

	// Validate roles.
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

	// Validate roles (same as create).
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

		// Validate instanceType based on active node provisioner.
		switch consts.ActiveNodeProvisioner {
		case consts.NodeProvisionerBYO:
			if role.InstanceType != "" {
				errs = errs.Also(apis.ErrInvalidValue(role.InstanceType, field+".instanceType",
					"instanceType must be empty when nodeProvisioner is byo"))
			}
		case consts.NodeProvisionerKarpenter, consts.NodeProvisionerAzureGPU:
			if role.InstanceType == "" {
				errs = errs.Also(apis.ErrMissingField(field + ".instanceType"))
			}
		default:
			// Unknown or unset provisioner: no validation (backward compat).
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
