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
	"strings"
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

// TestModelMirrorValidate_Static_Passes: a static mirror sets only Mode (no Source, no Storage)
// and should pass validation.
func TestModelMirrorValidate_Static_Passes(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Mode: ModelMirrorModeStatic,
			// Source and Storage intentionally omitted.
		},
	}

	if err := m.Validate(context.Background()); err != nil {
		t.Errorf("expected nil error for static mirror, got: %v", err)
	}
}

// TestModelMirrorValidate_Static_WithSource_Fails: a static mirror must not set Source
// (the weights already exist in BYO storage; there is nothing to download).
func TestModelMirrorValidate_Static_WithSource_Fails(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Mode:   ModelMirrorModeStatic,
			Source: &ModelMirrorSource{Registry: RegistryHuggingFace, ModelID: "org/model"},
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error when a static mirror sets spec.source, got nil")
	}
}

// TestModelMirrorValidate_Static_WithStorage_Fails: a static mirror must not set Storage
// (it creates no PVC).
func TestModelMirrorValidate_Static_WithStorage_Fails(t *testing.T) {
	client := fake.NewClientBuilder().
		WithScheme(newStorageScheme()).
		WithRuntimeObjects(storageClass("blob-fuse")).
		Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Mode:    ModelMirrorModeStatic,
			Storage: &ModelMirrorStorage{StorageClassName: ptr.To("blob-fuse"), Size: "85Gi"},
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error when a static mirror sets spec.storage, got nil")
	}
}

// TestModelMirrorValidate_Managed_StillValidates: a managed mirror with a StorageClass
// that exists in the cluster should pass validation.
func TestModelMirrorValidate_Managed_StillValidates(t *testing.T) {
	client := fake.NewClientBuilder().
		WithScheme(newStorageScheme()).
		WithRuntimeObjects(storageClass("blob-fuse")).
		Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Mode:         ModelMirrorModeManaged,
			Source:       &ModelMirrorSource{Registry: RegistryHuggingFace, ModelID: "org/model"},
			Storage:      &ModelMirrorStorage{StorageClassName: ptr.To("blob-fuse"), Size: "20Gi"},
			JobNamespace: "default",
		},
	}

	if err := m.Validate(context.Background()); err != nil {
		t.Errorf("expected nil error when StorageClass exists, got: %v", err)
	}
}

// TestModelMirrorValidate_Managed_MissingSource_Fails: a managed mirror with no Source must fail.
func TestModelMirrorValidate_Managed_MissingSource_Fails(t *testing.T) {
	client := fake.NewClientBuilder().
		WithScheme(newStorageScheme()).
		WithRuntimeObjects(storageClass("blob-fuse")).
		Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Mode:         ModelMirrorModeManaged,
			Storage:      &ModelMirrorStorage{StorageClassName: ptr.To("blob-fuse"), Size: "20Gi"},
			JobNamespace: "default",
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error when a managed mirror omits spec.source, got nil")
	}
}

// TestModelMirrorValidate_Managed_MissingStorage_Fails: a managed mirror with no Storage must fail.
func TestModelMirrorValidate_Managed_MissingStorage_Fails(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Mode:         ModelMirrorModeManaged,
			Source:       &ModelMirrorSource{Registry: RegistryHuggingFace, ModelID: "org/model"},
			JobNamespace: "default",
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error when a managed mirror omits spec.storage, got nil")
	}
}

// TestModelMirrorValidate_Managed_MissingStorageClass_Fails: a managed mirror with a
// StorageClassName that does not exist in the cluster should return an error.
func TestModelMirrorValidate_Managed_MissingStorageClass_Fails(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Mode:         ModelMirrorModeManaged,
			Source:       &ModelMirrorSource{Registry: RegistryHuggingFace, ModelID: "org/model"},
			Storage:      &ModelMirrorStorage{StorageClassName: ptr.To("nonexistent"), Size: "20Gi"},
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
			Mode:    ModelMirrorModeManaged,
			Source:  &ModelMirrorSource{Registry: "gcs", ModelID: "org/model"},
			Storage: &ModelMirrorStorage{StorageClassName: ptr.To("blob-fuse"), Size: "20Gi"},
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error for unsupported registry, got nil")
	}
}

// TestModelMirrorValidate_Managed_NilStorageClass_Fails: a managed mirror with nil
// StorageClassName must fail validation because it requires a PVC-backed StorageClass.
// An empty Mode defaults to Managed, so this also covers the default case.
func TestModelMirrorValidate_Managed_NilStorageClass_Fails(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			// Mode intentionally empty -> defaults to Managed
			Source:       &ModelMirrorSource{Registry: RegistryHuggingFace, ModelID: "org/model"},
			Storage:      &ModelMirrorStorage{Size: "20Gi"}, // StorageClassName intentionally nil
			JobNamespace: "default",
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error when managed StorageClassName is nil, got nil")
	}
}

// TestModelMirrorValidate_Managed_EmptyStorageClass_Fails: a managed mirror with an
// empty-string StorageClassName must also fail validation.
func TestModelMirrorValidate_Managed_EmptyStorageClass_Fails(t *testing.T) {
	client := fake.NewClientBuilder().WithScheme(newStorageScheme()).Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Mode:         ModelMirrorModeManaged,
			Source:       &ModelMirrorSource{Registry: RegistryHuggingFace, ModelID: "org/model"},
			Storage:      &ModelMirrorStorage{StorageClassName: ptr.To(""), Size: "20Gi"},
			JobNamespace: "default",
		},
	}

	if err := m.Validate(context.Background()); err == nil {
		t.Error("expected error when managed StorageClassName is empty string, got nil")
	}
}

// TestModelMirrorValidate_Managed_EmptyValues_ReportMissing: empty (blank) registry, modelID,
// and size must all report "missing field(s)" (ErrMissingField), not "invalid value"
// (ErrInvalidValue) — consistent treatment of blank required subfields.
func TestModelMirrorValidate_Managed_EmptyValues_ReportMissing(t *testing.T) {
	client := fake.NewClientBuilder().
		WithScheme(newStorageScheme()).
		WithRuntimeObjects(storageClass("blob-fuse")).
		Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Mode:         ModelMirrorModeManaged,
			Source:       &ModelMirrorSource{Registry: "", ModelID: ""},                        // both blank
			Storage:      &ModelMirrorStorage{Size: "", StorageClassName: ptr.To("blob-fuse")}, // blank size
			JobNamespace: "default",
		},
	}

	err := m.Validate(context.Background())
	if err == nil {
		t.Fatal("expected error for blank required subfields, got nil")
	}
	msg := err.Error()
	// All three blank fields should surface as missing, and their paths should be named.
	for _, path := range []string{"spec.source.registry", "spec.source.modelID", "spec.storage.size"} {
		if !strings.Contains(msg, path) {
			t.Errorf("expected error to name %q, got: %v", path, msg)
		}
	}
	if !strings.Contains(msg, "missing field") {
		t.Errorf("expected a 'missing field(s)' error for blank values, got: %v", msg)
	}
	if strings.Contains(msg, "invalid value") {
		t.Errorf("blank required values must not report 'invalid value', got: %v", msg)
	}
}

// TestModelMirrorValidate_Managed_BadSize_ReportsInvalid: a present-but-unparsable size
// reports "invalid value" (ErrInvalidValue), distinct from the blank case above.
func TestModelMirrorValidate_Managed_BadSize_ReportsInvalid(t *testing.T) {
	client := fake.NewClientBuilder().
		WithScheme(newStorageScheme()).
		WithRuntimeObjects(storageClass("blob-fuse")).
		Build()
	k8sclient.SetGlobalClient(client)

	m := &ModelMirror{
		Spec: ModelMirrorSpec{
			Mode:         ModelMirrorModeManaged,
			Source:       &ModelMirrorSource{Registry: RegistryHuggingFace, ModelID: "org/model"},
			Storage:      &ModelMirrorStorage{Size: "not-a-quantity", StorageClassName: ptr.To("blob-fuse")},
			JobNamespace: "default",
		},
	}

	err := m.Validate(context.Background())
	if err == nil {
		t.Fatal("expected error for unparsable size, got nil")
	}
	if !strings.Contains(err.Error(), "invalid value") {
		t.Errorf("expected 'invalid value' for a non-empty bad size, got: %v", err)
	}
}
