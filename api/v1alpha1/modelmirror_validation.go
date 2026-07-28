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
	"slices"
	"strings"

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

	// Mode defaults to Managed via the CRD schema; an empty value is treated as Managed.
	switch m.Spec.Mode {
	case ModelMirrorModeStatic:
		return m.validateStaticMirror()
	default:
		return m.validateManagedMirror(ctx)
	}
}

// validateStaticMirror validates a static mirror: the weights already exist in pre-existing
// (BYO) storage, so the user fills in nothing but Mode — Source and Storage must be absent.
func (m *ModelMirror) validateStaticMirror() (errs *apis.FieldError) {
	if m.Spec.Source != nil {
		errs = errs.Also(apis.ErrDisallowedFields("spec.source"))
	}
	if m.Spec.Storage != nil {
		errs = errs.Also(apis.ErrDisallowedFields("spec.storage"))
	}
	return errs
}

// validateManagedMirror validates a managed mirror: it downloads the model to a PVC, so Source
// and Storage are required and their subfields are validated. For each subfield, check-empty-first
// (missing) then value-check only non-empty values.
func (m *ModelMirror) validateManagedMirror(ctx context.Context) (errs *apis.FieldError) {
	if m.Spec.Source == nil {
		errs = errs.Also(apis.ErrMissingField("spec.source"))
	} else {
		if m.Spec.Source.Registry == "" {
			errs = errs.Also(apis.ErrMissingField("spec.source.registry"))
		} else if !slices.Contains(SupportedRegistries, m.Spec.Source.Registry) {
			supported := `"` + strings.Join(SupportedRegistries, `", "`) + `"`
			errs = errs.Also(apis.ErrInvalidValue(
				fmt.Sprintf("%q is not supported, only %s are supported", m.Spec.Source.Registry, supported),
				"spec.source.registry"))
		}
		if m.Spec.Source.ModelID == "" {
			errs = errs.Also(apis.ErrMissingField("spec.source.modelID"))
		}
	}

	if m.Spec.Storage == nil {
		errs = errs.Also(apis.ErrMissingField("spec.storage"))
		return errs
	}
	if m.Spec.Storage.Size == "" {
		errs = errs.Also(apis.ErrMissingField("spec.storage.size"))
	} else if _, err := resource.ParseQuantity(m.Spec.Storage.Size); err != nil {
		errs = errs.Also(apis.ErrInvalidValue(m.Spec.Storage.Size, "spec.storage.size"))
	}
	// A Managed mirror downloads to a PVC and requires an existing StorageClass.
	if m.Spec.Storage.StorageClassName == nil || *m.Spec.Storage.StorageClassName == "" {
		errs = errs.Also(apis.ErrMissingField("spec.storage.storageClassName"))
	} else {
		sc := &storagev1.StorageClass{}
		if err := k8sclient.Client.Get(ctx, types.NamespacedName{Name: *m.Spec.Storage.StorageClassName}, sc); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(*m.Spec.Storage.StorageClassName, "spec.storage.storageClassName"))
		}
	}
	return errs
}
