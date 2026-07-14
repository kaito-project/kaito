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

package modelstreaming

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// coreShape returns a map with all CORE-required SAS annotations set (the minimal set that
// activates the SAS path).
func coreShape() map[string]string {
	return map[string]string{
		AnnotationStreamURI:              "az://c/model",
		AnnotationStreamAccount:          "acct",
		AnnotationStreamDatarefsURL:      "https://x/datarefs",
		AnnotationStreamBlobURI:          "https://acct.blob.core.windows.net/c/prefix",
		AnnotationStreamIdentityClientID: "00000000-0000-0000-0000-000000000000",
	}
}

// publicShape returns coreShape plus the optional keys (assetId + audience).
func publicShape() map[string]string {
	m := coreShape()
	m[AnnotationStreamAssetID] = "azureml://registries/r/models/m/versions/1"
	m[AnnotationStreamTokenAudience] = "https://management.azure.com"
	return m
}

func TestHasSASBlobStreamingAnnotations(t *testing.T) {
	// Core-only (BYO shape) and public shape both satisfy Has.
	assert.True(t, HasSASBlobStreamingAnnotations(coreShape()))
	assert.True(t, HasSASBlobStreamingAnnotations(publicShape()))

	assert.False(t, HasSASBlobStreamingAnnotations(nil))
	assert.False(t, HasSASBlobStreamingAnnotations(map[string]string{}))

	// Dropping a CORE key -> not satisfied.
	partial := coreShape()
	delete(partial, AnnotationStreamIdentityClientID)
	assert.False(t, HasSASBlobStreamingAnnotations(partial))

	// Dropping an OPTIONAL key (assetId) from the public shape -> still satisfied (core intact).
	optDropped := publicShape()
	delete(optDropped, AnnotationStreamAssetID)
	assert.True(t, HasSASBlobStreamingAnnotations(optDropped))

	// Empty-string core value counts as absent.
	empty := coreShape()
	empty[AnnotationStreamURI] = ""
	assert.False(t, HasSASBlobStreamingAnnotations(empty))
}

func TestCountSASBlobStreamingAnnotations(t *testing.T) {
	// coreShape has all 5 core keys.
	assert.Equal(t, 5, CountSASBlobStreamingAnnotations(coreShape()))
	// publicShape adds only OPTIONAL keys, so the CORE count is still 5.
	assert.Equal(t, 5, CountSASBlobStreamingAnnotations(publicShape()))
	assert.Equal(t, 0, CountSASBlobStreamingAnnotations(nil))
	assert.Equal(t, 0, CountSASBlobStreamingAnnotations(map[string]string{}))
	// Only optional keys present -> 0 core.
	assert.Equal(t, 0, CountSASBlobStreamingAnnotations(map[string]string{AnnotationStreamAssetID: "x"}))

	partial := coreShape()
	delete(partial, AnnotationStreamIdentityClientID)
	assert.Equal(t, 4, CountSASBlobStreamingAnnotations(partial))
}

func TestRequireSASBlobStreamingAnnotations(t *testing.T) {
	// Valid: all core (BYO shape), core+optional (public shape), and none (mirror path).
	assert.NoError(t, RequireSASBlobStreamingAnnotations(coreShape()))
	assert.NoError(t, RequireSASBlobStreamingAnnotations(publicShape()))
	assert.NoError(t, RequireSASBlobStreamingAnnotations(nil))
	assert.NoError(t, RequireSASBlobStreamingAnnotations(map[string]string{}))

	// Optional-only (assetId) does NOT trigger the SAS path -> no error (treated as mirror path).
	assert.NoError(t, RequireSASBlobStreamingAnnotations(map[string]string{AnnotationStreamAssetID: "x"}))

	// One CORE key missing -> error naming that key, "4 of 5".
	oneMissing := coreShape()
	delete(oneMissing, AnnotationStreamIdentityClientID)
	err := RequireSASBlobStreamingAnnotations(oneMissing)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), AnnotationStreamIdentityClientID)
	assert.Contains(t, err.Error(), "4 of 5")

	// Two CORE keys missing -> both named, "3 of 5".
	twoMissing := coreShape()
	delete(twoMissing, AnnotationStreamIdentityClientID)
	delete(twoMissing, AnnotationStreamBlobURI)
	err2 := RequireSASBlobStreamingAnnotations(twoMissing)
	assert.Error(t, err2)
	assert.Contains(t, err2.Error(), AnnotationStreamIdentityClientID)
	assert.Contains(t, err2.Error(), AnnotationStreamBlobURI)
	assert.Contains(t, err2.Error(), "3 of 5")
}
