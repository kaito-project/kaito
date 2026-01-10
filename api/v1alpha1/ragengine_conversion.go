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

package v1alpha1

import (
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	"github.com/kaito-project/kaito/api/v1beta1"
)

// ConvertTo converts this RAGEngine (v1alpha1) to the Hub version (v1beta1).
func (src *RAGEngine) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta1.RAGEngine)

	// Copy TypeMeta
	dst.TypeMeta = src.TypeMeta
	// Copy ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Convert Spec
	if src.Spec != nil {
		dst.Spec = &v1beta1.RAGEngineSpec{}

		// Convert Compute
		dst.Spec.Compute = (*v1beta1.ResourceSpec)(src.Spec.Compute)

		// Convert Storage: v1alpha1 flat -> v1beta1 nested
		if src.Spec.Storage != nil {
			dst.Spec.Storage = &v1beta1.StorageSpec{}
			if src.Spec.Storage.PersistentVolumeClaim != "" || src.Spec.Storage.MountPath != "" {
				dst.Spec.Storage.PersistentVolume = &v1beta1.PersistentVolumeConfig{
					PersistentVolumeClaim: src.Spec.Storage.PersistentVolumeClaim,
					MountPath:             src.Spec.Storage.MountPath,
				}
			}
		}

		// Convert Embedding
		if src.Spec.Embedding != nil {
			dst.Spec.Embedding = &v1beta1.EmbeddingSpec{}
			if src.Spec.Embedding.Remote != nil {
				dst.Spec.Embedding.Remote = &v1beta1.RemoteEmbeddingSpec{
					URL:          src.Spec.Embedding.Remote.URL,
					AccessSecret: src.Spec.Embedding.Remote.AccessSecret,
				}
			}
			if src.Spec.Embedding.Local != nil {
				dst.Spec.Embedding.Local = &v1beta1.LocalEmbeddingSpec{
					Image:             src.Spec.Embedding.Local.Image,
					ImagePullSecret:   src.Spec.Embedding.Local.ImagePullSecret,
					ModelID:           src.Spec.Embedding.Local.ModelID,
					ModelAccessSecret: src.Spec.Embedding.Local.ModelAccessSecret,
				}
			}
		}

		// Convert InferenceService
		if src.Spec.InferenceService != nil {
			dst.Spec.InferenceService = &v1beta1.InferenceServiceSpec{
				URL:               src.Spec.InferenceService.URL,
				AccessSecret:      src.Spec.InferenceService.AccessSecret,
				ContextWindowSize: src.Spec.InferenceService.ContextWindowSize,
			}
		}

		// Note: QueryServiceName and IndexServiceName are v1alpha1-only fields, not converted
	}

	// Convert Status
	dst.Status = v1beta1.RAGEngineStatus{
		WorkerNodes: src.Status.WorkerNodes,
		Conditions:  src.Status.Conditions,
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1beta1) to this version (v1alpha1).
func (dst *RAGEngine) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta1.RAGEngine)

	// Copy TypeMeta
	dst.TypeMeta = src.TypeMeta
	// Copy ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Convert Spec
	if src.Spec != nil {
		dst.Spec = &RAGEngineSpec{}

		// Convert Compute
		dst.Spec.Compute = (*ResourceSpec)(src.Spec.Compute)

		// Convert Storage: v1beta1 nested -> v1alpha1 flat
		if src.Spec.Storage != nil {
			dst.Spec.Storage = &StorageSpec{}
			if src.Spec.Storage.PersistentVolume != nil {
				dst.Spec.Storage.PersistentVolumeClaim = src.Spec.Storage.PersistentVolume.PersistentVolumeClaim
				dst.Spec.Storage.MountPath = src.Spec.Storage.PersistentVolume.MountPath
			}
		}

		// Convert Embedding
		if src.Spec.Embedding != nil {
			dst.Spec.Embedding = &EmbeddingSpec{}
			if src.Spec.Embedding.Remote != nil {
				dst.Spec.Embedding.Remote = &RemoteEmbeddingSpec{
					URL:          src.Spec.Embedding.Remote.URL,
					AccessSecret: src.Spec.Embedding.Remote.AccessSecret,
				}
			}
			if src.Spec.Embedding.Local != nil {
				dst.Spec.Embedding.Local = &LocalEmbeddingSpec{
					Image:             src.Spec.Embedding.Local.Image,
					ImagePullSecret:   src.Spec.Embedding.Local.ImagePullSecret,
					ModelID:           src.Spec.Embedding.Local.ModelID,
					ModelAccessSecret: src.Spec.Embedding.Local.ModelAccessSecret,
				}
			}
		}

		// Convert InferenceService
		if src.Spec.InferenceService != nil {
			dst.Spec.InferenceService = &InferenceServiceSpec{
				URL:               src.Spec.InferenceService.URL,
				AccessSecret:      src.Spec.InferenceService.AccessSecret,
				ContextWindowSize: src.Spec.InferenceService.ContextWindowSize,
			}
		}

		// QueryServiceName and IndexServiceName are v1alpha1-only fields, left empty
	}

	// Convert Status
	dst.Status = RAGEngineStatus{
		WorkerNodes: src.Status.WorkerNodes,
		Conditions:  src.Status.Conditions,
	}

	return nil
}
