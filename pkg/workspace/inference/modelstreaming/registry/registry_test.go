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
	// A static model mirror selects the SAS blob provider (streams from a pre-existing blob).
	ws := &v1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ws1",
			Namespace: "default",
			Annotations: map[string]string{
				modelstreaming.AnnotationStaticModelMirror: "true",
			},
		},
	}
	_, ok := SelectModelStreamer(ws).(*azure.SASBlobProvider)
	assert.True(t, ok, "expected SASBlobProvider for a static model mirror")

	// SAS annotations present but NOT static -> normal download path (default provider),
	// because the SAS provider can only read a pre-existing blob, not a mirror-created PVC.
	modelstreaming.StreamingDefaults.ModelStreamer = &azure.WIBlobProvider{}
	sasNotStatic := &v1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ws-sas-not-static",
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
	_, ok = SelectModelStreamer(sasNotStatic).(*azure.WIBlobProvider)
	assert.True(t, ok, "expected the default provider when SAS annotations are present but static is off")

	// A workspace with no annotations falls back to the cluster default.
	plain := &v1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "default"}}
	_, ok = SelectModelStreamer(plain).(*azure.WIBlobProvider)
	assert.True(t, ok, "expected the default provider when no annotations are present")
}

func TestGetModelStreamer(t *testing.T) {
	// Azure is supported.
	s, err := GetModelStreamer("azure")
	assert.NoError(t, err)
	_, ok := s.(*azure.WIBlobProvider)
	assert.True(t, ok, "expected WIBlobProvider for cloud=azure")

	// Unsupported cloud returns an error.
	_, err = GetModelStreamer("gcp")
	assert.Error(t, err)
}
