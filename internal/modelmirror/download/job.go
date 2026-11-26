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

package download

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/internal/image"
)

// BuildDownloadJob constructs the Job that downloads model files to the PVC.
// resources sets the CPU/memory requests and limits on the downloader container.
// podLabels are applied to the Job pod template when a ServiceAccount is set (e.g. the
// cloud workload-identity label); pass nil to add none.
func BuildDownloadJob(cr *kaitov1alpha1.ModelMirror, resources DownloadJobResources, podLabels map[string]string) *batchv1.Job {
	modelID := cr.Spec.Source.ModelID
	// Match hf download's local directory; Kubernetes mount paths do not expand env vars.
	cacheMount := corev1.VolumeMount{
		Name: "hf-local-cache", MountPath: fmt.Sprintf("/models/%s/.cache/huggingface", modelID),
	}

	// Build --exclude flags from DownloadExcludePatterns
	excludeFlags := ""
	for _, pattern := range DownloadExcludePatterns {
		excludeFlags += fmt.Sprintf("\n  --exclude %q \\", pattern)
	}

	// Cleanup runs only after hf download succeeds. Preserve the local cache mount
	// and its .cache ancestor, but remove their contents and other directories
	// that would otherwise become zero-byte objects on blob storage.
	script := fmt.Sprintf(`set -e
export HF_HUB_DOWNLOAD_TIMEOUT=300

%s
pip install -q "huggingface-hub==%s" "certifi==%s"

# The native Xet client needs a complete, current CA bundle for the CAS endpoint.
# Resolve it separately from export so set -e catches a failed certifi lookup.
SSL_CERT_FILE="$(python3 -m certifi)"
export SSL_CERT_FILE

hf download "${MODEL_ID}" \
  --max-workers 4 \%s
  --local-dir "/models/${MODEL_ID}"

# Empty the cache without removing the mount root or its .cache ancestor.
find "/models/${MODEL_ID}/.cache/huggingface" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + 2>/dev/null || true
# Prune .cache so the PVC cleanup cannot traverse the mount or delete its ancestor.
find "/models/${MODEL_ID}/" -mindepth 1 -path "/models/${MODEL_ID}/.cache" -prune -o -type d -prune -exec rm -rf -- {} + 2>/dev/null || true`,
		image.PipBootstrapCommand,
		HuggingFaceHubVersion,
		CertifiVersion,
		excludeFlags,
	)

	envVars := []corev1.EnvVar{
		{Name: "MODEL_ID", Value: modelID},
	}

	// Always declare HF_TOKEN — optional:true means Kubernetes silently skips it
	// if the secret doesn't exist.
	if cr.Spec.Source.AccessSecretName != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name: "HF_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: cr.Spec.Source.AccessSecretName},
					Key:                  "HF_TOKEN",
					Optional:             ptr.To(true),
				},
			},
		})
	}

	container := corev1.Container{
		Name:    "downloader",
		Image:   image.PythonHelperImage,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{script},
		Env:     envVars,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(resources.CPU),
				corev1.ResourceMemory: resource.MustParse(resources.Memory),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(resources.CPULimit),
				corev1.ResourceMemory: resource.MustParse(resources.MemoryLimit),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "model-storage", MountPath: "/models"},
			cacheMount,
		},
	}

	// Create the nested mount destination before the sampler mounts /models
	// read-only. Otherwise runc cannot create it on a fresh PVC. This ordinary
	// init container mounts only the writable PVC and must finish first.
	prepare := corev1.Container{
		Name:    "prepare-model-cache",
		Image:   image.PythonHelperImage,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{`mkdir -p "/models/${MODEL_ID}/.cache/huggingface"`},
		Env:     []corev1.EnvVar{{Name: "MODEL_ID", Value: modelID}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("16Mi"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "model-storage", MountPath: "/models"},
		},
	}

	sampler := buildSamplerContainer(envVars)
	samplerCacheMount := cacheMount
	samplerCacheMount.ReadOnly = true
	sampler.VolumeMounts = append(sampler.VolumeMounts, samplerCacheMount)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: cr.Name + "-download-",
			Namespace:    cr.Namespace,
			Labels: map[string]string{
				kaitov1alpha1.LabelModelMirrorName: cr.Name,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(3)),
			TTLSecondsAfterFinished: ptr.To(int32(3600)), // 1 hour — keeps failed pods for debugging
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:  corev1.RestartPolicyNever,
					Containers:     []corev1.Container{container},
					InitContainers: []corev1.Container{prepare, sampler},
					Volumes: []corev1.Volume{
						{
							Name:         "hf-local-cache",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
						{
							Name: "model-storage",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: cr.Name,
								},
							},
						},
					},
				},
			},
		},
	}

	// When a ServiceAccount is set, run the Job under it and apply the provider-supplied pod
	// labels (e.g. the cloud workload-identity label). Empty leaves the pod on the namespace
	// default ServiceAccount with no extra labels.
	if cr.Spec.ServiceAccountName != "" {
		job.Spec.Template.Spec.ServiceAccountName = cr.Spec.ServiceAccountName
		for k, v := range podLabels {
			if job.Spec.Template.Labels == nil {
				job.Spec.Template.Labels = map[string]string{}
			}
			job.Spec.Template.Labels[k] = v
		}
	}
	return job
}
