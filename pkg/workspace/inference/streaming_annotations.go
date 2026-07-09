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
	"fmt"
	"strings"
)

// SAS-authenticated blob streaming annotations. When all five are present on a
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
)

// sasBlobStreamingAnnotationKeys is the full set of annotations required for the
// SAS-authenticated blob streaming path. All must be present and non-empty.
var sasBlobStreamingAnnotationKeys = []string{
	AnnotationStreamURI,
	AnnotationStreamAccount,
	AnnotationStreamDatarefsURL,
	AnnotationStreamAssetID,
	AnnotationStreamBlobURI,
}

// CountSASBlobStreamingAnnotations returns how many of the five stream-* annotations
// are present and non-empty.
func CountSASBlobStreamingAnnotations(annotations map[string]string) int {
	n := 0
	for _, k := range sasBlobStreamingAnnotationKeys {
		if annotations[k] != "" {
			n++
		}
	}
	return n
}

// HasSASBlobStreamingAnnotations reports whether all five stream-* annotations are
// present and non-empty.
func HasSASBlobStreamingAnnotations(annotations map[string]string) bool {
	return CountSASBlobStreamingAnnotations(annotations) == len(sasBlobStreamingAnnotationKeys)
}

// RequireSASBlobStreamingAnnotations enforces the no-fallback contract: if some but not
// all of the five stream-* annotations are present, it returns an error naming the missing
// ones. It returns nil when all five are present (valid SAS path) or when none are present
// (valid mirror path).
func RequireSASBlobStreamingAnnotations(annotations map[string]string) error {
	n := CountSASBlobStreamingAnnotations(annotations)
	if n == 0 || n == len(sasBlobStreamingAnnotationKeys) {
		return nil
	}
	var missing []string
	for _, k := range sasBlobStreamingAnnotationKeys {
		if annotations[k] == "" {
			missing = append(missing, k)
		}
	}
	return fmt.Errorf("incomplete SAS blob streaming configuration: %d of %d stream annotations set; "+
		"missing: %s (all must be set together, or none)",
		n, len(sasBlobStreamingAnnotationKeys), strings.Join(missing, ", "))
}
