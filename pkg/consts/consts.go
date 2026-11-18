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

// Package consts holds public values shared across KAITO packages and
// repositories that have no single owning package.
package consts

// Cloud provider names.
const (
	AzureCloudName = "azure"
	AWSCloudName   = "aws"
	ArcCloudName   = "arc"
)

// CSIDriverNameAzureBlob is the CSI driver used for model streaming on Azure.
const CSIDriverNameAzureBlob = "blob.csi.azure.com"

// CSIDriverNameForCloud returns the expected CSI driver name for the given cloud provider.
// Returns "" for unsupported providers.
func CSIDriverNameForCloud(cloud string) string {
	switch cloud {
	case AzureCloudName:
		return CSIDriverNameAzureBlob
	default:
		return ""
	}
}
