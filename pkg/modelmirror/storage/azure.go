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

package storage

import (
	"fmt"
	"strings"
)

// AzureBlobProvider implements CloudProvider for Azure Blob Storage.
type AzureBlobProvider struct{}

// NewAzureBlobProvider creates an Azure Blob Storage cloud provider.
func NewAzureBlobProvider() *AzureBlobProvider {
	return &AzureBlobProvider{}
}

func (p *AzureBlobProvider) CSIDriverName() string {
	return "blob.csi.azure.com"
}

// ParseVolumeHandle extracts storage account and container name from an Azure Blob
// CSI volumeHandle. Format: <resourceGroup>#<storageAccount>#<containerName>##<namespace>#
func (p *AzureBlobProvider) ParseVolumeHandle(volumeHandle string) (accountName, containerName string, err error) {
	parts := strings.Split(volumeHandle, "#")
	if len(parts) < 3 {
		return "", "", fmt.Errorf("invalid volumeHandle format: expected at least 3 '#'-separated parts, got %d in %q", len(parts), volumeHandle)
	}
	accountName = parts[1]
	containerName = parts[2]
	if accountName == "" || containerName == "" {
		return "", "", fmt.Errorf("invalid volumeHandle: accountName or containerName is empty in %q", volumeHandle)
	}
	return accountName, containerName, nil
}

func (p *AzureBlobProvider) BuildStorageURI(containerName, modelID string) string {
	return fmt.Sprintf("az://%s/%s", containerName, modelID)
}
