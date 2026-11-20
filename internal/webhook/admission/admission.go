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

// Package admission builds KAITO's validating admission controllers.
package admission

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	knativeinjection "knative.dev/pkg/injection"
	"knative.dev/pkg/webhook/resourcesemantics"
	"knative.dev/pkg/webhook/resourcesemantics/validation"

	"github.com/kaito-project/kaito/api/env"
)

// NewValidationController returns a constructor for a validating admission
// controller whose request contexts carry e.
func NewValidationController(e *env.Env, name, path string, resources map[schema.GroupVersionKind]resourcesemantics.GenericCRD) knativeinjection.ControllerConstructor {
	return func(ctx context.Context, _ configmap.Watcher) *controller.Impl {
		return validation.NewAdmissionController(ctx, name, path, resources,
			func(ctx context.Context) context.Context { return env.WithEnv(ctx, e) }, true)
	}
}
