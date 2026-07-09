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

package inference

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func allFive() map[string]string {
	return map[string]string{
		AnnotationStreamURI:         "az://c/model",
		AnnotationStreamAccount:     "acct",
		AnnotationStreamDatarefsURL: "https://x/datarefs",
		AnnotationStreamAssetID:     "azureml://registries/r/models/m/versions/1",
		AnnotationStreamBlobURI:     "https://acct.blob.core.windows.net/c/prefix",
	}
}

func TestHasSASBlobStreamingAnnotations(t *testing.T) {
	assert.True(t, HasSASBlobStreamingAnnotations(allFive()))
	assert.False(t, HasSASBlobStreamingAnnotations(nil))
	assert.False(t, HasSASBlobStreamingAnnotations(map[string]string{}))

	partial := allFive()
	delete(partial, AnnotationStreamAssetID)
	assert.False(t, HasSASBlobStreamingAnnotations(partial))

	empty := allFive()
	empty[AnnotationStreamURI] = ""
	assert.False(t, HasSASBlobStreamingAnnotations(empty))
}

func TestCountSASBlobStreamingAnnotations(t *testing.T) {
	assert.Equal(t, 5, CountSASBlobStreamingAnnotations(allFive()))
	assert.Equal(t, 0, CountSASBlobStreamingAnnotations(nil))
	assert.Equal(t, 0, CountSASBlobStreamingAnnotations(map[string]string{}))
	partial := allFive()
	delete(partial, AnnotationStreamAssetID)
	assert.Equal(t, 4, CountSASBlobStreamingAnnotations(partial))
}

func TestRequireSASBlobStreamingAnnotations(t *testing.T) {
	assert.NoError(t, RequireSASBlobStreamingAnnotations(allFive()))

	partial := allFive()
	delete(partial, AnnotationStreamAssetID)
	err := RequireSASBlobStreamingAnnotations(partial)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), AnnotationStreamAssetID)

	twoMissing := allFive()
	delete(twoMissing, AnnotationStreamAssetID)
	delete(twoMissing, AnnotationStreamBlobURI)
	err2 := RequireSASBlobStreamingAnnotations(twoMissing)
	assert.Error(t, err2)
	assert.Contains(t, err2.Error(), AnnotationStreamAssetID)
	assert.Contains(t, err2.Error(), AnnotationStreamBlobURI)
	assert.Contains(t, err2.Error(), "3 of 5")

	assert.NoError(t, RequireSASBlobStreamingAnnotations(nil))
	assert.NoError(t, RequireSASBlobStreamingAnnotations(map[string]string{}))
}
