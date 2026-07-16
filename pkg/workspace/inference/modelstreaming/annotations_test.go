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

func TestValidateStaticModelMirrorAnnotations(t *testing.T) {
	// Static disabled -> never checks the SAS annotations, always nil (even with a partial set).
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(nil))
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(map[string]string{}))
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(publicShape())) // full SAS set but static off -> ignored
	partialNoStatic := coreShape()
	delete(partialNoStatic, AnnotationStreamBlobURI)
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(partialNoStatic)) // partial + static off -> ignored

	withStatic := func(m map[string]string) map[string]string {
		m[AnnotationStaticModelMirror] = "true"
		return m
	}

	// Static enabled + all core SAS annotations -> nil.
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(withStatic(coreShape())))

	// Static enabled + NONE of the SAS annotations -> error (naming all core keys).
	errNone := ValidateStaticModelMirrorAnnotations(map[string]string{AnnotationStaticModelMirror: "true"})
	assert.Error(t, errNone)
	assert.Contains(t, errNone.Error(), AnnotationStaticModelMirror)
	assert.Contains(t, errNone.Error(), AnnotationStreamURI)
	assert.Contains(t, errNone.Error(), AnnotationStreamIdentityClientID)

	// Static enabled + PARTIAL SAS set -> error (naming the missing one).
	partial := coreShape()
	delete(partial, AnnotationStreamBlobURI)
	errPartial := ValidateStaticModelMirrorAnnotations(withStatic(partial))
	assert.Error(t, errPartial)
	assert.Contains(t, errPartial.Error(), AnnotationStreamBlobURI)
}

func TestStaticModelMirrorEnabled(t *testing.T) {
	assert.False(t, StaticModelMirrorEnabled(nil))
	assert.False(t, StaticModelMirrorEnabled(map[string]string{}))
	assert.False(t, StaticModelMirrorEnabled(map[string]string{AnnotationStaticModelMirror: "false"}))
	assert.False(t, StaticModelMirrorEnabled(map[string]string{AnnotationStaticModelMirror: "True"})) // only exact "true"
	assert.True(t, StaticModelMirrorEnabled(map[string]string{AnnotationStaticModelMirror: "true"}))
}
