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
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	kaitoapis "github.com/kaito-project/kaito/pkg/apis"
)

// ragEngineValidator bridges controller-runtime's CustomValidator interface
// to the existing RAGEngine.Validate(ctx) (*apis.FieldError) method. The
// shape mirrors pkg/workspace/webhooks; both will be folded onto a shared
// helper if a third validator ever lands.
type ragEngineValidator struct{}

var _ admission.CustomValidator = (*ragEngineValidator)(nil)

func (ragEngineValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*kaitov1beta1.RAGEngine)
	if !ok {
		return nil, fmt.Errorf("expected v1beta1.RAGEngine, got %T", obj)
	}
	if fe := r.Validate(ctx); fe != nil {
		return nil, fmt.Errorf("RAGEngine validation failed: %w", fe)
	}
	return nil, nil
}

func (ragEngineValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*kaitov1beta1.RAGEngine)
	if !ok {
		return nil, fmt.Errorf("expected v1beta1.RAGEngine, got %T", newObj)
	}
	ctx = kaitoapis.WithinUpdate(ctx, oldObj)
	if fe := r.Validate(ctx); fe != nil {
		return nil, fmt.Errorf("RAGEngine validation failed: %w", fe)
	}
	return nil, nil
}

func (ragEngineValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// SetupWebhooksWithManager registers the RAGEngine validating admission
// webhook against mgr. cert-controller is responsible for serving the TLS
// material; this function only wires up the handler.
func SetupWebhooksWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewWebhookManagedBy(mgr).
		For(&kaitov1beta1.RAGEngine{}).
		WithValidator(ragEngineValidator{}).
		Complete(); err != nil {
		return fmt.Errorf("register v1beta1 RAGEngine webhook: %w", err)
	}
	return nil
}
