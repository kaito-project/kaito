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

package azure

import (
	"fmt"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming"
)

// GetModelStreamer returns the ModelStreamer implementation for the given cloud.
// Currently only Azure is supported. To add a new provider, add a case here
// and in consts.CSIDriverNameForCloud().
func GetModelStreamer(cloudName string) (modelstreaming.ModelStreamer, error) {
	switch cloudName {
	case consts.AzureCloudName:
		return &AzureBlobProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported cloud provider %q for model streaming; supported: azure", cloudName)
	}
}

// SelectModelStreamer picks the streaming provider for a workspace: the SAS blob
// provider when the workspace carries the SAS blob streaming annotations, otherwise
// the cluster default (set at startup via modelstreaming.StreamingDefaults).
func SelectModelStreamer(ws *v1beta1.Workspace) modelstreaming.ModelStreamer {
	if modelstreaming.HasSASBlobStreamingAnnotations(ws.Annotations) {
		return &SASBlobProvider{}
	}
	return modelstreaming.StreamingDefaults.ModelStreamer
}
