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

// SAS-authenticated blob streaming annotations. When all core annotations are present on a
// Workspace (with model streaming enabled), KAITO streams weights directly from a
// pre-existing external blob using a short-lived SAS token minted at pod start,
// instead of mirroring the model to a PVC.
//
// These belong to the streaming path, not the mirror path: mirroring is independent of
// streaming (it only copies weights to a PVC, skipping the download when no StorageClass
// is set), so it has no knowledge of these keys.
const (
	AnnotationStreamURI         = "inference.kaito.sh/stream-uri"          // vLLM --model (az://…, subpath baked in)
	AnnotationStreamAccount     = "inference.kaito.sh/stream-account"      // AZURE_STORAGE_ACCOUNT_NAME
	AnnotationStreamDatarefsURL = "inference.kaito.sh/stream-datarefs-url" // POST target to mint a fresh SAS
	AnnotationStreamAssetID     = "inference.kaito.sh/stream-asset-id"     // POST body {assetId}
	AnnotationStreamBlobURI     = "inference.kaito.sh/stream-blob-uri"     // POST body {blobUri}
	// AnnotationStreamIdentityClientID is the workload identity client ID used to mint the SAS.
	AnnotationStreamIdentityClientID = "inference.kaito.sh/stream-identity-client-id" // WI client id for token exchange
	// AnnotationStreamTokenAudience is the AAD token audience for the SAS-mint call. Optional;
	AnnotationStreamTokenAudience = "inference.kaito.sh/stream-token-audience" // AAD audience for get_token

	// AnnotationStaticModelMirror, when set to "true", marks the workspace as using a STATIC
	// model mirror: enabling this flag requires the core SAS annotations to be present.
	AnnotationStaticModelMirror = "inference.kaito.sh/static-model-mirror" // "true" => Mode=Static
)

// coreSASBlobStreamingAnnotationKeys is the set of annotations REQUIRED to activate the SAS
// blob streaming path. They must all be present together (SAS path) or all absent (mirror path).
// assetId and token-audience are optional
var coreSASBlobStreamingAnnotationKeys = []string{
	AnnotationStreamURI,
	AnnotationStreamAccount,
	AnnotationStreamDatarefsURL,
	AnnotationStreamBlobURI,
	AnnotationStreamIdentityClientID,
}

// ValidateStaticModelMirrorAnnotations enforces the static-mirror contract: when the static flag
// is enabled, all core SAS streaming annotations must be present (a partial set or none both fail).
// When the flag is disabled, the SAS annotations are not checked at all.
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
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%s=true requires all core SAS streaming annotations; missing: %s",
		AnnotationStaticModelMirror, strings.Join(missing, ", "))
}

// StaticModelMirrorEnabled reports whether the workspace opts into a static model mirror.
func StaticModelMirrorEnabled(annotations map[string]string) bool {
	return annotations[AnnotationStaticModelMirror] == "true"
}
