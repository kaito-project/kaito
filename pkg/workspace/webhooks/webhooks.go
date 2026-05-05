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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	kaitoapis "github.com/kaito-project/kaito/pkg/apis"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// kaitoValidator is the bridge between controller-runtime's CustomValidator
// interface and the existing Knative-shaped Validate(ctx) (*apis.FieldError)
// methods that live on each KAITO API type. Each call site narrows the
// generic object to its concrete type via the validate closure so that we get
// a useful error if the apiserver dispatches an object of the wrong kind.
type kaitoValidator struct {
	kind     string
	validate func(ctx context.Context, obj runtime.Object) *kaitoapis.FieldError
}

var _ admission.CustomValidator = (*kaitoValidator)(nil)

// ValidateCreate runs the wrapped Validate against obj with no baseline on
// the context, matching the create path of the existing Validate methods.
func (v *kaitoValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	if fe := v.validate(ctx, obj); fe != nil {
		return nil, fmt.Errorf("%s validation failed: %w", v.kind, fe)
	}
	return nil, nil
}

// ValidateUpdate stores the prior object on the context as the baseline so
// that Validate(ctx) takes its update path (matches Knative's WithinUpdate
// semantics).
func (v *kaitoValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	ctx = kaitoapis.WithinUpdate(ctx, oldObj)
	if fe := v.validate(ctx, newObj); fe != nil {
		return nil, fmt.Errorf("%s validation failed: %w", v.kind, fe)
	}
	return nil, nil
}

// ValidateDelete is a no-op: KAITO never blocked deletes through the Knative
// webhook either, and there is no Validate(ctx) variant for delete on the
// existing types.
func (v *kaitoValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// SetupWebhooksWithManager registers all Workspace and (optionally) InferenceSet
// validating admission webhooks against mgr's webhook server. cert-controller
// is responsible for serving / rotating the TLS material; this function only
// wires up the handlers.
func SetupWebhooksWithManager(mgr ctrl.Manager) error {
	if err := setupValidator(mgr, &kaitov1beta1.Workspace{}, &kaitoValidator{
		kind: "Workspace",
		validate: func(ctx context.Context, obj runtime.Object) *kaitoapis.FieldError {
			ws, ok := obj.(*kaitov1beta1.Workspace)
			if !ok {
				return kaitoapis.ErrGeneric(fmt.Sprintf("expected v1beta1.Workspace, got %T", obj))
			}
			return ws.Validate(ctx)
		},
	}); err != nil {
		return fmt.Errorf("register v1beta1 Workspace webhook: %w", err)
	}

	if err := setupValidator(mgr, &kaitov1alpha1.Workspace{}, &kaitoValidator{
		kind: "Workspace",
		validate: func(ctx context.Context, obj runtime.Object) *kaitoapis.FieldError {
			ws, ok := obj.(*kaitov1alpha1.Workspace)
			if !ok {
				return kaitoapis.ErrGeneric(fmt.Sprintf("expected v1alpha1.Workspace, got %T", obj))
			}
			return ws.Validate(ctx)
		},
	}); err != nil {
		return fmt.Errorf("register v1alpha1 Workspace webhook: %w", err)
	}

	if featuregates.FeatureGates[consts.FeatureFlagEnableInferenceSetController] {
		if err := setupValidator(mgr, &kaitov1alpha1.InferenceSet{}, &kaitoValidator{
			kind: "InferenceSet",
			validate: func(ctx context.Context, obj runtime.Object) *kaitoapis.FieldError {
				is, ok := obj.(*kaitov1alpha1.InferenceSet)
				if !ok {
					return kaitoapis.ErrGeneric(fmt.Sprintf("expected v1alpha1.InferenceSet, got %T", obj))
				}
				return is.Validate(ctx)
			},
		}); err != nil {
			return fmt.Errorf("register v1alpha1 InferenceSet webhook: %w", err)
		}
	}

	return nil
}

// setupValidator builds a controller-runtime managed-webhook for obj using v.
// It is centralised so that all KAITO validators get identical handler setup.
func setupValidator(mgr ctrl.Manager, obj runtime.Object, v admission.CustomValidator) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(obj).
		WithValidator(v).
		Complete()
}

// WebhookServer returns the underlying webhook server so callers (cmd/main.go)
// can configure cert paths after cert-controller has populated them.
func WebhookServer(mgr ctrl.Manager) webhook.Server {
	return mgr.GetWebhookServer()
}
