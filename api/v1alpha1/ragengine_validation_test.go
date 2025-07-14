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
	"strings"
	"testing"

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
						InstanceType: "Standard_NC12s_v3",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com"},
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
						InstanceType: "Standard_NC12s_v3",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com"},
				},
			},
			wantErr:  true,
			errField: "Embedding must be specified",
		},
		{
			name: "None of Local and Remote Embedding specified",
			ragEngine: &RAGEngine{
				Spec: &RAGEngineSpec{
					Compute: &ResourceSpec{
						InstanceType: "Standard_NC12s_v3",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com"},
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
						InstanceType: "Standard_NC12s_v3",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com"},
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
						InstanceType: "Standard_NC12s_v3",
					},
					InferenceService: &InferenceServiceSpec{URL: "http://example.com"},
					Embedding: &EmbeddingSpec{
						Remote: &RemoteEmbeddingSpec{URL: "http://remote-embedding.com"},
					},
				},
			},
			wantErr: false,
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
				URL: "invalid-url",
			},
			wantErr:  true,
			errField: "URL input error",
		},
		{
			name: "Valid URL Specified",
			inferenceService: &InferenceServiceSpec{
				URL: "http://example.com",
			},
			wantErr: false,
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
