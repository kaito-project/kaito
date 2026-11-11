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

// Package chunksize provides per-model recommended run:ai streamer chunk sizes.
//
// The recommendations in chunksizes_generated.go are derived offline from each
// model's safetensors tensor layout (the modal per-tensor byte size) and are
// used to default RUNAI_STREAMER_CHUNK_BYTESIZE when caching a known model, so
// the dominant per-tensor read aligns with a single cache chunk. The list of
// models is in models.yaml; regenerate the Go file with:
//
//	make generate-chunksizes
package chunksize

import "strings"

// Recommended returns the recommended run:ai streamer chunk byte size for the
// given KAITO model name and true, or 0 and false if the model has no generated
// recommendation. The argument is normalized to match the generated keys: it is
// lowercased and reduced to the final path segment, so both the runtime model
// name (e.g. "mistral-7b-v0.3") and a full HuggingFace id (e.g.
// "mistralai/Mistral-7B-v0.3") resolve to the same entry. The returned value is
// suitable for RUNAI_STREAMER_CHUNK_BYTESIZE.
func Recommended(model string) (int, bool) {
	key := strings.ToLower(model)
	if i := strings.LastIndex(key, "/"); i >= 0 {
		key = key[i+1:]
	}
	v, ok := recommendedChunkSizes[key]
	return v, ok
}
