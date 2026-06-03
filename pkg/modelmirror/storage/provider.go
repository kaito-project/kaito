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

// CloudProvider abstracts cloud-specific storage operations for model mirroring.
// Implementing this interface for a new cloud (e.g. AWS S3, GCP GCS) is the only
// change needed to support model streaming on that cloud.
type CloudProvider interface {
	// CSIDriverName returns the CSI driver name to validate exists in the cluster.
	CSIDriverName() string
	// ParseVolumeHandle extracts provider-specific identifiers from a PV CSI volumeHandle.
	// Returns the storage account (or equivalent) and bucket/container name.
	ParseVolumeHandle(volumeHandle string) (accountName, bucketOrContainer string, err error)
	// BuildStorageURI constructs the streaming URI for RunAI model streamer
	// (e.g. "az://container/modelID", "s3://bucket/modelID", "gs://bucket/modelID").
	BuildStorageURI(bucketOrContainer, modelID string) string
}
