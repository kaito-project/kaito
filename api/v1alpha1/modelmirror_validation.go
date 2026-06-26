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

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/pkg/k8sclient"
)

func (m *ModelMirror) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (m *ModelMirror) Validate(ctx context.Context) (errs *apis.FieldError) {
	base := apis.GetBaseline(ctx)
	if base != nil {
		old := base.(*ModelMirror)
		if !reflect.DeepEqual(m.Spec, old.Spec) {
			errs = errs.Also(apis.ErrGeneric("ModelMirror spec is immutable; delete and recreate to change", "spec"))
		}
		return errs
	}

	// Value validation (presence enforced by CRD schema)
	if m.Spec.Source.Registry != "huggingface" {
		errs = errs.Also(apis.ErrInvalidValue(
			fmt.Sprintf("%q is not supported, only \"huggingface\" is supported", m.Spec.Source.Registry),
			"spec.source.registry"))
	}
	if _, err := resource.ParseQuantity(m.Spec.Storage.Size); err != nil {
		errs = errs.Also(apis.ErrInvalidValue(m.Spec.Storage.Size, "spec.storage.size"))
	}
	sc := &storagev1.StorageClass{}
	if err := k8sclient.Client.Get(ctx, types.NamespacedName{Name: m.Spec.Storage.StorageClassName}, sc); err != nil {
		errs = errs.Also(apis.ErrInvalidValue(m.Spec.Storage.StorageClassName, "spec.storage.storageClassName"))
	}
	return errs
}
