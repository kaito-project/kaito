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

	"github.com/kaito-project/kaito/api/v1beta1"
)

// coreShape returns a map with all CORE-required SAS annotations set (the minimal set that
// activates the SAS path). The blob-derived values are resolved at runtime, so they are not here.
func coreShape() map[string]string {
	return map[string]string{
		v1beta1.AnnotationStreamDatarefsURL:      "https://x/models/m/versions/1",
		v1beta1.AnnotationStreamIdentityClientID: "00000000-0000-0000-0000-000000000000",
		v1beta1.AnnotationStreamSourceType:       v1beta1.StreamSourceTypeBYO,
	}
}

func TestValidateStaticModelMirrorAnnotations(t *testing.T) {
	// Static disabled -> never checks the SAS annotations, always nil (even with a partial set).
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(nil))
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(map[string]string{}))
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(coreShape())) // full SAS set but static off -> ignored
	partialNoStatic := coreShape()
	delete(partialNoStatic, v1beta1.AnnotationStreamDatarefsURL)
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(partialNoStatic)) // partial + static off -> ignored

	withStatic := func(m map[string]string) map[string]string {
		m[v1beta1.AnnotationStaticModelMirror] = "true"
		return m
	}

	// Static enabled + all core SAS annotations -> nil.
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(withStatic(coreShape())))

	// Static enabled + NONE of the SAS annotations -> error (naming all core keys).
	errNone := ValidateStaticModelMirrorAnnotations(map[string]string{v1beta1.AnnotationStaticModelMirror: "true"})
	assert.Error(t, errNone)
	assert.Contains(t, errNone.Error(), v1beta1.AnnotationStaticModelMirror)
	assert.Contains(t, errNone.Error(), v1beta1.AnnotationStreamDatarefsURL)
	assert.Contains(t, errNone.Error(), v1beta1.AnnotationStreamIdentityClientID)

	// Static enabled + PARTIAL SAS set -> error (naming the missing one).
	partial := coreShape()
	delete(partial, v1beta1.AnnotationStreamDatarefsURL)
	errPartial := ValidateStaticModelMirrorAnnotations(withStatic(partial))
	assert.Error(t, errPartial)
	assert.Contains(t, errPartial.Error(), v1beta1.AnnotationStreamDatarefsURL)

	// Static enabled + all core present but invalid source type -> error.
	badType := withStatic(coreShape())
	badType[v1beta1.AnnotationStreamSourceType] = "bogus"
	errType := ValidateStaticModelMirrorAnnotations(badType)
	assert.Error(t, errType)
	assert.Contains(t, errType.Error(), v1beta1.AnnotationStreamSourceType)

	// public is a valid source type too.
	pub := withStatic(coreShape())
	pub[v1beta1.AnnotationStreamSourceType] = v1beta1.StreamSourceTypePublic
	assert.NoError(t, ValidateStaticModelMirrorAnnotations(pub))
}

func TestStaticModelMirrorEnabled(t *testing.T) {
	assert.False(t, StaticModelMirrorEnabled(nil))
	assert.False(t, StaticModelMirrorEnabled(map[string]string{}))
	assert.False(t, StaticModelMirrorEnabled(map[string]string{v1beta1.AnnotationStaticModelMirror: "false"}))
	assert.False(t, StaticModelMirrorEnabled(map[string]string{v1beta1.AnnotationStaticModelMirror: "True"})) // only exact "true"
	assert.True(t, StaticModelMirrorEnabled(map[string]string{v1beta1.AnnotationStaticModelMirror: "true"}))
}
