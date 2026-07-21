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
)

// SAS-authenticated blob streaming annotations. When the static-model-mirror flag and the core
// annotations are present on a Workspace (with model streaming enabled), KAITO streams weights
// directly from a pre-existing external blob using a short-lived SAS token minted at pod start,
// instead of mirroring the model to a PVC.

// These belong to the streaming path, not the mirror path: mirroring is independent of
// streaming (it only copies weights to a PVC, skipping the download when no StorageClass
// is set), so it has no knowledge of these keys.
const (
	AnnotationStreamDatarefsURL = "inference.kaito.sh/stream-datarefs-url" // POST target to mint a fresh SAS
	// AnnotationStreamIdentityClientID is the workload identity client ID used to mint the SAS.
	AnnotationStreamIdentityClientID = "inference.kaito.sh/stream-identity-client-id" // WI client id for token exchange
	// AnnotationStreamSourceType selects the model source API flavor: "public" or "byo". It
	// drives the model-resolve URL derivation and the token audience used to mint the SAS.
	AnnotationStreamSourceType = "inference.kaito.sh/stream-source-type" // "public" | "byo"

	// AnnotationStaticModelMirror, when set to "true", marks the workspace as using a STATIC
	// model mirror: enabling this flag requires the core SAS annotations to be present.
	AnnotationStaticModelMirror = "inference.kaito.sh/static-model-mirror" // "true" => Mode=Static
)

// Source type values for AnnotationStreamSourceType.
const (
	SourceTypePublic = "public"
	SourceTypeBYO    = "byo"
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
