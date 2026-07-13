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

package controllers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

func TestBuildStaticModelMirror(t *testing.T) {
	const (
		crName    = "qwen2-5-coder-32b"
		modelID   = "Qwen/Qwen2.5-Coder-32B-Instruct"
		modelSize = "64Gi"
	)
	accessSecret := &corev1.ObjectReference{Name: "my-secret", Namespace: "default"}

	cr := buildStaticModelMirror(crName, modelID, modelSize, accessSecret)

	if cr.Name != crName {
		t.Errorf("Name: got %q, want %q", cr.Name, crName)
	}

	if cr.Spec.Mode != kaitov1alpha1.ModelMirrorModeStatic {
		t.Errorf("Mode: got %q, want %q", cr.Spec.Mode, kaitov1alpha1.ModelMirrorModeStatic)
	}

	// A static mirror carries no streaming vocabulary; the mirror is agnostic to it.
	if len(cr.Annotations) != 0 {
		t.Errorf("Annotations: got %v, want none", cr.Annotations)
	}

	if cr.Spec.Source.Registry != kaitov1alpha1.RegistryAzureML {
		t.Errorf("Source.Registry: got %q, want %q", cr.Spec.Source.Registry, kaitov1alpha1.RegistryAzureML)
	}
	if cr.Spec.Source.ModelID != modelID {
		t.Errorf("Source.ModelID: got %q, want %q", cr.Spec.Source.ModelID, modelID)
	}
	if cr.Spec.Source.AccessSecret != accessSecret {
		t.Errorf("Source.AccessSecret: got %v, want %v", cr.Spec.Source.AccessSecret, accessSecret)
	}

	if cr.Spec.Storage.Size != modelSize {
		t.Errorf("Storage.Size: got %q, want %q", cr.Spec.Storage.Size, modelSize)
	}
	if cr.Spec.Storage.StorageClassName != nil {
		t.Errorf("Storage.StorageClassName: got %v, want nil (no PVC for a static mirror)", cr.Spec.Storage.StorageClassName)
	}

	// No PVC/Job is created for a static mirror, so JobNamespace must be empty.
	if cr.Spec.JobNamespace != "" {
		t.Errorf("JobNamespace: got %q, want empty (no Job for a static mirror)", cr.Spec.JobNamespace)
	}
}

func TestBuildStaticModelMirror_NilAccessSecret(t *testing.T) {
	cr := buildStaticModelMirror("model-cr", "org/model", "20Gi", nil)

	if cr.Spec.Source.AccessSecret != nil {
		t.Errorf("Source.AccessSecret: got %v, want nil", cr.Spec.Source.AccessSecret)
	}
}
