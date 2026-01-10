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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
)

func TestRAGEngineConversion(t *testing.T) {
	tests := []struct {
		name     string
		v1alpha1 *RAGEngine
	}{
		{
			name: "Convert with PVC storage",
			v1alpha1: &RAGEngine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rag",
					Namespace: "default",
				},
				Spec: &RAGEngineSpec{
					Storage: &StorageSpec{
						PersistentVolumeClaim: "my-pvc",
						MountPath:             "/mnt/data",
					},
					Embedding: &EmbeddingSpec{
						Local: &LocalEmbeddingSpec{
							ModelID: "BAAI/bge-small-en-v1.5",
						},
					},
					InferenceService: &InferenceServiceSpec{
						URL:               "http://inference-service/v1/chat",
						ContextWindowSize: 4096,
					},
				},
			},
		},
		{
			name: "Convert without storage",
			v1alpha1: &RAGEngine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rag-2",
					Namespace: "default",
				},
				Spec: &RAGEngineSpec{
					Embedding: &EmbeddingSpec{
						Remote: &RemoteEmbeddingSpec{
							URL:          "https://api.openai.com/v1/embeddings",
							AccessSecret: "openai-secret",
						},
					},
					InferenceService: &InferenceServiceSpec{
						URL:               "http://inference-service/v1/chat",
						ContextWindowSize: 8192,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert v1alpha1 -> v1beta1
			v1beta1Rag := &v1beta1.RAGEngine{}
			if err := tt.v1alpha1.ConvertTo(v1beta1Rag); err != nil {
				t.Fatalf("ConvertTo failed: %v", err)
			}

			// Verify basic fields
			if v1beta1Rag.Name != tt.v1alpha1.Name {
				t.Errorf("Name mismatch: got %s, want %s", v1beta1Rag.Name, tt.v1alpha1.Name)
			}

			// Verify storage conversion (flat -> nested)
			if tt.v1alpha1.Spec.Storage != nil {
				if v1beta1Rag.Spec.Storage == nil {
					t.Error("Storage should not be nil after conversion")
				} else if v1beta1Rag.Spec.Storage.PersistentVolume != nil {
					if v1beta1Rag.Spec.Storage.PersistentVolume.PersistentVolumeClaim != tt.v1alpha1.Spec.Storage.PersistentVolumeClaim {
						t.Errorf("PVC mismatch: got %s, want %s",
							v1beta1Rag.Spec.Storage.PersistentVolume.PersistentVolumeClaim,
							tt.v1alpha1.Spec.Storage.PersistentVolumeClaim)
					}
					if v1beta1Rag.Spec.Storage.PersistentVolume.MountPath != tt.v1alpha1.Spec.Storage.MountPath {
						t.Errorf("MountPath mismatch: got %s, want %s",
							v1beta1Rag.Spec.Storage.PersistentVolume.MountPath,
							tt.v1alpha1.Spec.Storage.MountPath)
					}
				}
			}

			// Convert back: v1beta1 -> v1alpha1
			roundTrip := &RAGEngine{}
			if err := roundTrip.ConvertFrom(v1beta1Rag); err != nil {
				t.Fatalf("ConvertFrom failed: %v", err)
			}

			// Verify storage round-trip (nested -> flat)
			if tt.v1alpha1.Spec.Storage != nil {
				if roundTrip.Spec.Storage == nil {
					t.Error("Storage should not be nil after round-trip")
				} else {
					if roundTrip.Spec.Storage.PersistentVolumeClaim != tt.v1alpha1.Spec.Storage.PersistentVolumeClaim {
						t.Errorf("Round-trip PVC mismatch: got %s, want %s",
							roundTrip.Spec.Storage.PersistentVolumeClaim,
							tt.v1alpha1.Spec.Storage.PersistentVolumeClaim)
					}
					if roundTrip.Spec.Storage.MountPath != tt.v1alpha1.Spec.Storage.MountPath {
						t.Errorf("Round-trip MountPath mismatch: got %s, want %s",
							roundTrip.Spec.Storage.MountPath,
							tt.v1alpha1.Spec.Storage.MountPath)
					}
				}
			}

			// Verify inference service
			if roundTrip.Spec.InferenceService.ContextWindowSize != tt.v1alpha1.Spec.InferenceService.ContextWindowSize {
				t.Errorf("ContextWindowSize mismatch: got %d, want %d",
					roundTrip.Spec.InferenceService.ContextWindowSize,
					tt.v1alpha1.Spec.InferenceService.ContextWindowSize)
			}
		})
	}
}
