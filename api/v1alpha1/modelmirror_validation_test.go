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
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/pkg/k8sclient"
)

func newStorageScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = storagev1.AddToScheme(scheme)
	return scheme
}

func storageClass(name string) *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

// TestModelMirrorValidate_StreamOnly_Passes: azureml registry with nil StorageClassName
// and empty JobNamespace should pass validation without requiring a StorageClass.
func TestModelMirrorValidate_StreamOnly_Passes(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Source:  ModelMirrorSource{Registry: "azureml", ModelID: "org/model"},
			Storage: ModelMirrorStorage{Size: "85Gi"},
			// JobNamespace intentionally empty
		},
	}

	if err := m.Validate(context.Background()); err != nil {
		t.Errorf("expected nil error for stream-only mirror, got: %v", err)
	}
}

// TestModelMirrorValidate_Mirror_StillValidates: huggingface with a StorageClass that
// exists in the cluster should pass validation.
func TestModelMirrorValidate_Mirror_StillValidates(t *testing.T) {
	client := fake.NewClientBuilder().
		WithScheme(newStorageScheme()).
		WithRuntimeObjects(storageClass("blob-fuse")).
		Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Source:       ModelMirrorSource{Registry: "huggingface", ModelID: "org/model"},
			Storage:      ModelMirrorStorage{StorageClassName: ptr.To("blob-fuse"), Size: "20Gi"},
			JobNamespace: "default",
		},
	}

	if err := m.Validate(context.Background()); err != nil {
		t.Errorf("expected nil error when StorageClass exists, got: %v", err)
	}
}

// TestModelMirrorValidate_Mirror_MissingStorageClass_Fails: huggingface with a
// StorageClassName that does not exist in the cluster should return an error.
func TestModelMirrorValidate_Mirror_MissingStorageClass_Fails(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Source:       ModelMirrorSource{Registry: "huggingface", ModelID: "org/model"},
			Storage:      ModelMirrorStorage{StorageClassName: ptr.To("nonexistent"), Size: "20Gi"},
			JobNamespace: "default",
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error when StorageClass is missing, got nil")
	}
}

// TestModelMirrorValidate_BadRegistry_Fails: an unsupported registry value should
// return a validation error.
func TestModelMirrorValidate_BadRegistry_Fails(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Source:  ModelMirrorSource{Registry: "gcs", ModelID: "org/model"},
			Storage: ModelMirrorStorage{Size: "20Gi"},
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error for unsupported registry, got nil")
	}
}

// TestModelMirrorValidate_HuggingFace_NilStorageClass_Fails: a huggingface CR with
// nil StorageClassName must fail validation because it requires a PVC-backed StorageClass.
func TestModelMirrorValidate_HuggingFace_NilStorageClass_Fails(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Source:       ModelMirrorSource{Registry: "huggingface", ModelID: "org/model"},
			Storage:      ModelMirrorStorage{Size: "20Gi"}, // StorageClassName intentionally nil
			JobNamespace: "default",
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error when huggingface StorageClassName is nil, got nil")
	}
}

// TestModelMirrorValidate_HuggingFace_EmptyStorageClass_Fails: a huggingface CR with
// an empty-string StorageClassName must also fail validation.
func TestModelMirrorValidate_HuggingFace_EmptyStorageClass_Fails(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Source:       ModelMirrorSource{Registry: "huggingface", ModelID: "org/model"},
			Storage:      ModelMirrorStorage{StorageClassName: ptr.To(""), Size: "20Gi"},
			JobNamespace: "default",
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error when huggingface StorageClassName is empty string, got nil")
	}
}
