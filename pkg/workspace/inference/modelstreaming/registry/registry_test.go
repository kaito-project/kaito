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

package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming/azure"
)

func TestSelectModelStreamer(t *testing.T) {
	// A workspace carrying the core SAS blob streaming annotations selects the SAS provider.
	ws := &v1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ws1",
			Namespace: "default",
			Annotations: map[string]string{
				modelstreaming.AnnotationStreamURI:              "az://c/model",
				modelstreaming.AnnotationStreamAccount:          "acct",
				modelstreaming.AnnotationStreamDatarefsURL:      "https://x/datarefs",
				modelstreaming.AnnotationStreamBlobURI:          "https://acct.blob.core.windows.net/c/prefix",
				modelstreaming.AnnotationStreamIdentityClientID: "11111111-2222-3333-4444-555555555555",
			},
		},
	}
	_, ok := SelectModelStreamer(ws).(*azure.SASBlobProvider)
	assert.True(t, ok, "expected SASBlobProvider when the core SAS annotations are present")

	// A workspace with no stream annotations falls back to the cluster default.
	modelstreaming.StreamingDefaults.ModelStreamer = &azure.AzureBlobProvider{}
	plain := &v1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "default"}}
	_, ok = SelectModelStreamer(plain).(*azure.AzureBlobProvider)
	assert.True(t, ok, "expected the default provider when no stream annotations are present")
}

func TestGetModelStreamer(t *testing.T) {
	// Azure is supported.
	s, err := GetModelStreamer("azure")
	assert.NoError(t, err)
	_, ok := s.(*azure.AzureBlobProvider)
	assert.True(t, ok, "expected AzureBlobProvider for cloud=azure")

	// Unsupported cloud returns an error.
	_, err = GetModelStreamer("gcp")
	assert.Error(t, err)
}
