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
	"fmt"
	"strings"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// SAS-authenticated blob streaming annotation keys and source-type values.
//
// These are aliases of the canonical definitions in pkg/utils/consts, which holds them in a
// leaf package so that API validation can reference the same keys without importing this
// package (which depends on the API types). They are re-exported here because this is where
// the streaming path reads them.
const (
	AnnotationStreamDatarefsURL      = consts.AnnotationStreamDatarefsURL
	AnnotationStreamIdentityClientID = consts.AnnotationStreamIdentityClientID
	AnnotationStreamSourceType       = consts.AnnotationStreamSourceType
	AnnotationStaticModelMirror      = consts.AnnotationStaticModelMirror

	SourceTypePublic = consts.SourceTypePublic
	SourceTypeBYO    = consts.SourceTypeBYO
)

// coreSASBlobStreamingAnnotationKeys is the set of annotations REQUIRED to activate the SAS
// blob streaming path. The blob URI, storage account, and model streaming URI are derived at runtime
// by the init container.
var coreSASBlobStreamingAnnotationKeys = []string{
	AnnotationStreamDatarefsURL,
	AnnotationStreamIdentityClientID,
	AnnotationStreamSourceType,
}

// ValidateStaticModelMirrorAnnotations enforces the static-mirror contract: when the static flag
// is enabled, all core SAS streaming annotations must be present (a partial set or none both fail)
// and the source type must be a supported value.
func ValidateStaticModelMirrorAnnotations(annotations map[string]string) error {
	if !StaticModelMirrorEnabled(annotations) {
		return nil
	}
	var missing []string
	for _, k := range coreSASBlobStreamingAnnotationKeys {
		if annotations[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s=true requires all core SAS streaming annotations; missing: %s",
			AnnotationStaticModelMirror, strings.Join(missing, ", "))
	}
	if ft := annotations[AnnotationStreamSourceType]; ft != SourceTypePublic && ft != SourceTypeBYO {
		return fmt.Errorf("%s must be %q or %q, got %q",
			AnnotationStreamSourceType, SourceTypePublic, SourceTypeBYO, ft)
	}
	return nil
}

// StaticModelMirrorEnabled reports whether the workspace opts into a static model mirror.
func StaticModelMirrorEnabled(annotations map[string]string) bool {
	return annotations[AnnotationStaticModelMirror] == "true"
}
