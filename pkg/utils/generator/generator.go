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

package generator

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
)

type WorkspaceGeneratorContext struct {
	Ctx        context.Context
	Workspace  *v1beta1.Workspace
	Model      pkgmodel.Model
	KubeClient client.Client
}

type GeneratorContext interface {
	WorkspaceGeneratorContext
}

type ManifestType interface {
	appsv1.StatefulSet | appsv1.Deployment | batchv1.Job | corev1.PodSpec
}

type TypedManifestModifier[C GeneratorContext, T ManifestType] func(ctx *C, obj *T) error

func GenerateManifest[C GeneratorContext, T ManifestType](ctx *C, modifiers ...TypedManifestModifier[C, T]) (*T, error) {
	var manifest T
	for _, m := range modifiers {
		if err := m(ctx, &manifest); err != nil {
			return nil, fmt.Errorf("failed to apply modifier: %w", err)
		}
	}
	return &manifest, nil
}
