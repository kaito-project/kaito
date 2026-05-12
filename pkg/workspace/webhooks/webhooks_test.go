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

package webhooks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	kaitoapis "github.com/kaito-project/kaito/pkg/apis"
)

// TestKaitoValidatorTypeMismatch ensures the bridge rejects unexpected types
// rather than panicking when the apiserver dispatches the wrong kind.
func TestKaitoValidatorTypeMismatch(t *testing.T) {
	v := &kaitoValidator{
		kind: "Workspace",
		validate: func(_ context.Context, obj runtime.Object) *kaitoapis.FieldError {
			if _, ok := obj.(*kaitov1beta1.Workspace); !ok {
				return kaitoapis.ErrGeneric("expected v1beta1.Workspace")
			}
			return nil
		},
	}

	// A v1alpha1 Workspace is the wrong type for this validator instance.
	wrong := &kaitov1alpha1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	_, err := v.ValidateCreate(context.Background(), wrong)
	require.Error(t, err)
}

// TestKaitoValidatorDeleteIsNoop confirms ValidateDelete never blocks delete
// requests, matching the previous Knative configuration which only registered
// Create + Update.
func TestKaitoValidatorDeleteIsNoop(t *testing.T) {
	v := &kaitoValidator{
		kind: "Workspace",
		validate: func(_ context.Context, _ runtime.Object) *kaitoapis.FieldError {
			t.Fatal("validate must not be called on delete")
			return nil
		},
	}
	warnings, err := v.ValidateDelete(context.Background(), &kaitov1beta1.Workspace{})
	require.NoError(t, err)
	assert.Nil(t, warnings)
}

// TestKaitoValidatorUpdateSetsBaseline checks that ValidateUpdate stashes the
// old object on the context where the underlying Validate(ctx) method can
// retrieve it via apis.GetBaseline (the create-vs-update signal).
func TestKaitoValidatorUpdateSetsBaseline(t *testing.T) {
	old := &kaitov1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "old"}}
	newer := &kaitov1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "new"}}

	var sawBaseline runtime.Object
	v := &kaitoValidator{
		kind: "Workspace",
		validate: func(ctx context.Context, _ runtime.Object) *kaitoapis.FieldError {
			if b := kaitoapis.GetBaseline(ctx); b != nil {
				sawBaseline = b.(runtime.Object)
			}
			return nil
		},
	}

	_, err := v.ValidateUpdate(context.Background(), old, newer)
	require.NoError(t, err)
	assert.Same(t, old, sawBaseline, "ValidateUpdate must seed apis.GetBaseline with the old object")

	// Create path must NOT have a baseline.
	sawBaseline = nil
	_, err = v.ValidateCreate(context.Background(), newer)
	require.NoError(t, err)
	assert.Nil(t, sawBaseline, "ValidateCreate must not seed a baseline")
}
