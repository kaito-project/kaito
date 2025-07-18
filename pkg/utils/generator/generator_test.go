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
	"testing"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
)

func TestGenerator(t *testing.T) {
	wctx := &WorkspaceGeneratorContext{
		Ctx: context.Background(),
		Workspace: &v1beta1.Workspace{
			Inference: &v1beta1.InferenceSpec{
				Preset: &v1beta1.PresetSpec{
					PresetMeta: v1beta1.PresetMeta{
						Name: "test-preset",
					},
				},
			},
		},
	}

	res, err := GenerateManifest(wctx,
		func(ctx *WorkspaceGeneratorContext, obj *appsv1.StatefulSet) error {
			obj.Namespace = "test-namespace"
			obj.Name = "test-statefulset"
			obj.Spec.Replicas = lo.ToPtr[int32](2)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("failed to generate manifest: %v", err)
	}
	if res.Namespace != "test-namespace" {
		t.Errorf("expected namespace 'test-namespace', got '%s'", res.Namespace)
	}
	if res.Name != "test-statefulset" {
		t.Errorf("expected name 'test-statefulset', got '%s'", res.Name)
	}
	if *res.Spec.Replicas != 2 {
		t.Errorf("expected replicas 2, got %d", *res.Spec.Replicas)
	}

}
