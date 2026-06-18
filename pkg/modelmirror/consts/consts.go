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

package consts

const (
	// Finalizers
	ModelMirrorFinalizer    = "kaito.sh/model-mirror-cleanup"
	ModelMirrorPVCFinalizer = "kaito.sh/model-mirror-protection"

	// Annotations
	AnnotationModelStreaming          = "kaito.sh/model-streaming"
	AnnotationModelMirrorStorageClass = "kaito.sh/model-mirror-storage-class"
	AnnotationStreamingServiceAccount = "kaito.sh/streaming-service-account"

	// Conditions
	ConditionTypeStorageReady = "StorageReady"
	ConditionTypeReady        = "Ready"

	// Labels
	LabelModelMirrorName = "kaito.sh/model-mirror-name"

	// Downloader image
	DownloaderImage = "mcr.microsoft.com/mirror/docker/library/python:3.11-slim"

	// huggingface-hub version
	HuggingFaceHubVersion = "1.18.0"
)

// DownloadExcludePatterns is the list of glob patterns to exclude from HF downloads.
var DownloadExcludePatterns = []string{"original/*"}
