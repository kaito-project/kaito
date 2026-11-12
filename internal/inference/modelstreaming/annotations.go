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

	"github.com/kaito-project/kaito/api/v1beta1"
)

// coreSASBlobStreamingAnnotationKeys is the set of annotations REQUIRED to activate the SAS
// blob streaming path. The blob URI, storage account, and model streaming URI are derived at runtime
// by the init container.
var coreSASBlobStreamingAnnotationKeys = []string{
	v1beta1.AnnotationStreamDatarefsURL,
	v1beta1.AnnotationStreamIdentityClientID,
	v1beta1.AnnotationStreamSourceType,
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
			v1beta1.AnnotationStaticModelMirror, strings.Join(missing, ", "))
	}
	if ft := annotations[v1beta1.AnnotationStreamSourceType]; ft != v1beta1.StreamSourceTypePublic && ft != v1beta1.StreamSourceTypeBYO {
		return fmt.Errorf("%s must be %q or %q, got %q",
			v1beta1.AnnotationStreamSourceType, v1beta1.StreamSourceTypePublic, v1beta1.StreamSourceTypeBYO, ft)
	}
	return nil
}

// StaticModelMirrorEnabled reports whether the workspace opts into a static model mirror.
func StaticModelMirrorEnabled(annotations map[string]string) bool {
	return annotations[v1beta1.AnnotationStaticModelMirror] == "true"
}
