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

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/pkg/apis"
)

func (m *ModelMirror) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (m *ModelMirror) Validate(_ context.Context) (errs *apis.FieldError) {
	if m.Spec.Source.ModelID == "" {
		errs = errs.Also(apis.ErrMissingField("spec.source.modelID"))
	}
	if m.Spec.Source.Registry == "" {
		errs = errs.Also(apis.ErrMissingField("spec.source.registry"))
	} else if m.Spec.Source.Registry != "huggingface" {
		errs = errs.Also(apis.ErrInvalidValue(m.Spec.Source.Registry, "spec.source.registry"))
	}
	if m.Spec.Storage.StorageSize != "" {
		if _, err := resource.ParseQuantity(m.Spec.Storage.StorageSize); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(m.Spec.Storage.StorageSize, "spec.storage.storageSize"))
		}
	}
	return errs
}
