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

package v1beta1

import (
	"context"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestRAGEngineValidateCreate(t *testing.T) {
	tests := []struct {
		name      string
		ragEngine *RAGEngine
		wantErr   bool
		errField  string
	}{
		{
			name: "Both Local and Remote Embedding specified",
			ragEngine: &RAGEngine{
				Spec: &RAGEngineSpec{
					Compute: &ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com", ContextWindowSize: 512},
					Embedding: &EmbeddingSpec{
						Local: &LocalEmbeddingSpec{
							ModelID: "BAAI/bge-small-en-v1.5",
						},
						Remote: &RemoteEmbeddingSpec{URL: "http://remote-embedding.com"},
					},
				},
			},
			wantErr:  true,
			errField: "Either remote embedding or local embedding must be specified, but not both",
		},
		{
			name: "Embedding not specified",
			ragEngine: &RAGEngine{
				Spec: &RAGEngineSpec{
					Compute: &ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com", ContextWindowSize: 512},
				},
			},
			wantErr:  true,
			errField: "Embedding must be specified",
		},
		{
			name: "Inference Service not specified",
			ragEngine: &RAGEngine{
				Spec: &RAGEngineSpec{
					Compute: &ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
					},
					Embedding: &EmbeddingSpec{
						Local: &LocalEmbeddingSpec{
							ModelID: "BAAI/bge-small-en-v1.5",
						},
					},
					InferenceService: nil,
				},
			},
			wantErr: false,
		},
		{
			name: "Compute resource not specified",
			ragEngine: &RAGEngine{
				Spec: &RAGEngineSpec{
					Compute: nil,
					Embedding: &EmbeddingSpec{
						Local: &LocalEmbeddingSpec{
							ModelID: "BAAI/bge-small-en-v1.5",
						},
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com", ContextWindowSize: 512},
				},
			},
			wantErr: false,
		},
		{
			name: "None of Local and Remote Embedding specified",
			ragEngine: &RAGEngine{
				Spec: &RAGEngineSpec{
					Compute: &ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com", ContextWindowSize: 512},
					Embedding:        &EmbeddingSpec{},
				},
			},
			wantErr:  true,
			errField: "Either remote embedding or local embedding must be specified, not neither",
		},
		{
			name: "Only Local Embedding specified",
			ragEngine: &RAGEngine{
				Spec: &RAGEngineSpec{
					Compute: &ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com", ContextWindowSize: 512},
					Embedding: &EmbeddingSpec{
						Local: &LocalEmbeddingSpec{
							ModelID: "BAAI/bge-small-en-v1.5",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Only Remote Embedding specified",
			ragEngine: &RAGEngine{
				Spec: &RAGEngineSpec{
					Compute: &ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com", ContextWindowSize: 512},
					Embedding: &EmbeddingSpec{
						Remote: &RemoteEmbeddingSpec{URL: "http://remote-embedding.com"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Only Remote Embedding with invalid context window size",
			ragEngine: &RAGEngine{
				Spec: &RAGEngineSpec{
					Compute: &ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com", ContextWindowSize: 0},
					Embedding: &EmbeddingSpec{
						Remote: &RemoteEmbeddingSpec{URL: "http://remote-embedding.com"},
					},
				},
			},
			wantErr:  true,
			errField: "ContextWindowSize must be a positive integer",
		},
	}
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ragEngine.validateCreate()
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Errorf("validateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if hasErr && tt.errField != "" && !strings.Contains(err.Error(), tt.errField) {
				t.Errorf("validateCreate() expected error to contain %s, but got %s", tt.errField, err.Error())
			}
		})
	}
}

func TestStorageSpecValidate(t *testing.T) {
	tests := []struct {
		name      string
		mountPath string
		wantErr   bool
	}{
		{name: "default mount path", mountPath: ""},
		{name: "storage root", mountPath: "/mnt/data"},
		{name: "nested storage directory", mountPath: "/mnt/data/customer/indexes"},
		{name: "historical storage directory", mountPath: "/mnt/vector-db"},
		{name: "custom storage directory", mountPath: "/mnt/customer/data"},
		{name: "relative path", mountPath: "mnt/data", wantErr: true},
		{name: "different root", mountPath: "/etc", wantErr: true},
		{name: "mount root", mountPath: "/mnt", wantErr: true},
		{name: "sibling prefix", mountPath: "/mnt-other/data", wantErr: true},
		{name: "parent traversal", mountPath: "/mnt/data/../../../etc", wantErr: true},
		{name: "nested parent traversal", mountPath: "/mnt/data/customer/../other", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &StorageSpec{
				PersistentVolume: &PersistentVolumeConfig{MountPath: tt.mountPath},
			}

			err := storage.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLocalEmbeddingValidateCreate(t *testing.T) {
	tests := []struct {
		name           string
		localEmbedding *LocalEmbeddingSpec
		wantErr        bool
		errField       string
	}{
		{
			name:           "Neither Image nor ModelID specified",
			localEmbedding: &LocalEmbeddingSpec{},
			wantErr:        true,
			errField:       "Either image or modelID must be specified, not neither",
		},
		{
			name: "Both Image and ModelID specified",
			localEmbedding: &LocalEmbeddingSpec{
				Image:   "image-path",
				ModelID: "model-id",
			},
			wantErr:  true,
			errField: "Either image or modelID must be specified, but not both",
		},
		{
			name: "Invalid Image Format",
			localEmbedding: &LocalEmbeddingSpec{
				Image: "invalid-image-format",
			},
			wantErr:  true,
			errField: "Invalid image format",
		},
		{
			name: "Valid Image Specified",
			localEmbedding: &LocalEmbeddingSpec{
				Image: "myrepo/myimage:tag",
			},
			wantErr: false,
		},
		{
			name: "Valid ModelID Specified",
			localEmbedding: &LocalEmbeddingSpec{
				ModelID: "valid-model-id",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.localEmbedding.validateCreate()
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Errorf("validateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if hasErr && tt.errField != "" && !strings.Contains(err.Error(), tt.errField) {
				t.Errorf("validateCreate() expected error to contain %s, but got %s", tt.errField, err.Error())
			}
		})
	}
}

func TestRemoteEmbeddingValidateCreate(t *testing.T) {
	tests := []struct {
		name            string
		remoteEmbedding *RemoteEmbeddingSpec
		wantErr         bool
		errField        string
	}{
		{
			name: "Invalid URL Specified",
			remoteEmbedding: &RemoteEmbeddingSpec{
				URL: "invalid-url",
			},
			wantErr:  true,
			errField: "URL input error",
		},
		{
			name: "Valid URL Specified",
			remoteEmbedding: &RemoteEmbeddingSpec{
				URL: "http://example.com",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.remoteEmbedding.validateCreate()
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Errorf("validateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if hasErr && tt.errField != "" && !strings.Contains(err.Error(), tt.errField) {
				t.Errorf("validateCreate() expected error to contain %s, but got %s", tt.errField, err.Error())
			}
		})
	}
}

func TestRAGEngineValidateGuardrails(t *testing.T) {
	tests := []struct {
		name      string
		ragEngine *RAGEngine
		objects   []runtime.Object
		wantErr   string
	}{
		{
			name: "enabled guardrails without configmap ref uses default policy",
			ragEngine: &RAGEngine{
				ObjectMeta: metav1.ObjectMeta{Name: "test-rag", Namespace: "default"},
				Spec: &RAGEngineSpec{
					Embedding:  &EmbeddingSpec{Local: &LocalEmbeddingSpec{ModelID: "BAAI/bge-small-en-v1.5"}},
					Guardrails: &GuardrailsSpec{Enabled: true},
				},
			},
			objects: []runtime.Object{
				&v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: DefaultGuardrailsPolicyConfigMapName, Namespace: "kaito-system"}, Data: map[string]string{GuardrailsPolicyFileName: "action: block\nscanners: []\n"}},
			},
		},
		{
			name: "enabled guardrails without configmap ref reports missing default policy clearly",
			ragEngine: &RAGEngine{
				ObjectMeta: metav1.ObjectMeta{Name: "test-rag", Namespace: "default"},
				Spec: &RAGEngineSpec{
					Embedding:  &EmbeddingSpec{Local: &LocalEmbeddingSpec{ModelID: "BAAI/bge-small-en-v1.5"}},
					Guardrails: &GuardrailsSpec{Enabled: true},
				},
			},
			wantErr: "kubectl get configmap ragengine-guardrails-policy-template -n kaito-system",
		},
		{
			name: "missing guardrails policy file is rejected",
			ragEngine: &RAGEngine{
				ObjectMeta: metav1.ObjectMeta{Name: "test-rag", Namespace: "default"},
				Spec: &RAGEngineSpec{
					Embedding: &EmbeddingSpec{Local: &LocalEmbeddingSpec{ModelID: "BAAI/bge-small-en-v1.5"}},
					Guardrails: &GuardrailsSpec{
						Enabled:      true,
						ConfigMapRef: &ConfigMapReference{Name: "guardrails-policy"},
					},
				},
			},
			objects: []runtime.Object{
				&v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "guardrails-policy", Namespace: "default"}, Data: map[string]string{"other.yaml": "action: passthrough"}},
			},
			wantErr: "guardrails.yaml in ConfigMap",
		},
		{
			name: "missing explicit guardrails configmap reports configMapRef",
			ragEngine: &RAGEngine{
				ObjectMeta: metav1.ObjectMeta{Name: "test-rag", Namespace: "default"},
				Spec: &RAGEngineSpec{
					Embedding: &EmbeddingSpec{Local: &LocalEmbeddingSpec{ModelID: "BAAI/bge-small-en-v1.5"}},
					Guardrails: &GuardrailsSpec{
						Enabled:      true,
						ConfigMapRef: &ConfigMapReference{Name: "missing-guardrails-policy"},
					},
				},
			},
			wantErr: "guardrails.configMapRef.name references ConfigMap \"missing-guardrails-policy\"",
		},
		{
			name: "valid guardrails policy passes",
			ragEngine: &RAGEngine{
				ObjectMeta: metav1.ObjectMeta{Name: "test-rag", Namespace: "default"},
				Spec: &RAGEngineSpec{
					Embedding: &EmbeddingSpec{Local: &LocalEmbeddingSpec{ModelID: "BAAI/bge-small-en-v1.5"}},
					Guardrails: &GuardrailsSpec{
						Enabled:      true,
						ConfigMapRef: &ConfigMapReference{Name: "guardrails-policy"},
					},
				},
			},
			objects: []runtime.Object{
				&v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "guardrails-policy", Namespace: "default"}, Data: map[string]string{"guardrails.yaml": "action: redact\nscanners:\n  - type: toxicity\n  - type: bias\n"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)
			k8sclient.Client = ctrlclientfake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.objects...).Build()
			t.Setenv(consts.DefaultReleaseNamespaceEnvVar, "kaito-system")

			err := tt.ragEngine.validateGuardrails(context.Background())
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateGuardrails() unexpected error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateGuardrails() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestInferenceServiceValidateCreate(t *testing.T) {
	tests := []struct {
		name             string
		inferenceService *InferenceServiceSpec
		wantErr          bool
		errField         string
	}{
		{
			name: "Invalid URL Specified",
			inferenceService: &InferenceServiceSpec{
				URL:               "invalid-url",
				ContextWindowSize: 512,
			},
			wantErr:  true,
			errField: "URL input error",
		},
		{
			name: "Valid URL Specified",
			inferenceService: &InferenceServiceSpec{
				URL:               "http://example.com",
				ContextWindowSize: 512,
			},
			wantErr: false,
		},
		{
			name: "Invalid ContextWindowSize",
			inferenceService: &InferenceServiceSpec{
				URL:               "http://example.com",
				ContextWindowSize: 0,
			},
			wantErr:  true,
			errField: "ContextWindowSize must be a positive integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.inferenceService.validateCreate()
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Errorf("validateCreate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if hasErr && tt.errField != "" && !strings.Contains(err.Error(), tt.errField) {
				t.Errorf("validateCreate() expected error to contain %s, but got %s", tt.errField, err.Error())
			}
		})
	}
}
