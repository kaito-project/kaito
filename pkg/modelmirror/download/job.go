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
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
)

// BuildDownloadJob constructs the Job that downloads model files to the PVC.
// resources sets the CPU/memory request==limit on the downloader container.
// podLabels are applied to the Job pod template when a ServiceAccount is set (e.g. the
// cloud workload-identity label); pass nil to add none.
func BuildDownloadJob(cr *kaitov1alpha1.ModelMirror, resources mmconsts.DownloadJobResources, podLabels map[string]string) *batchv1.Job {
	modelID := cr.Spec.Source.ModelID

	// Build --exclude flags from DownloadExcludePatterns
	excludeFlags := ""
	for _, pattern := range mmconsts.DownloadExcludePatterns {
		excludeFlags += fmt.Sprintf("\n  --exclude %q \\", pattern)
	}

	// Post-download cleanup: empty directories left on the PVC become zero-byte
	// blob objects on Azure Blob NFS. RunAI model streamer iterates all objects in
	// the container and crashes on directories (IsADirectoryError). We remove all
	// subdirectories as a safety net.
	script := fmt.Sprintf(`set -e
export HF_HUB_ENABLE_HF_TRANSFER=1
export HF_HUB_DOWNLOAD_TIMEOUT=300

pip install -q "huggingface-hub==%s" hf_transfer

hf download "${MODEL_ID}" \
  --max-workers 4 \%s
  --local-dir "/models/${MODEL_ID}"

# Remove all subdirectories — on HNS-enabled blob (NFS), directories become
# zero-byte objects that cause RunAI model streamer to fail with FileExistsError.
rm -rf "/models/${MODEL_ID}/.cache" 2>/dev/null || true
find "/models/${MODEL_ID}/" -mindepth 1 -type d -exec rm -rf {} + 2>/dev/null || true`,
		mmconsts.HuggingFaceHubVersion,
		excludeFlags,
	)

	envVars := []corev1.EnvVar{
		{Name: "MODEL_ID", Value: modelID},
	}

	// Always declare HF_TOKEN — optional:true means Kubernetes silently skips it
	// if the secret doesn't exist.
	if cr.Spec.Source.AccessSecret != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name: "HF_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: cr.Spec.Source.AccessSecret.Name},
					Key:                  "HF_TOKEN",
					Optional:             ptr.To(true),
				},
			},
		})
	}

	container := corev1.Container{
		Name:    "downloader",
		Image:   mmconsts.DownloaderImage,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{script},
		Env:     envVars,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(resources.CPU),
				corev1.ResourceMemory: resource.MustParse(resources.Memory),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(resources.CPU),
				corev1.ResourceMemory: resource.MustParse(resources.Memory),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "model-storage", MountPath: "/models"},
		},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: cr.Name + "-download-",
			Namespace:    cr.Spec.JobNamespace,
			Labels: map[string]string{
				mmconsts.LabelModelMirrorName: cr.Name,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(3)),
			TTLSecondsAfterFinished: ptr.To(int32(3600)), // 1 hour — keeps failed pods for debugging
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers:    []corev1.Container{container},
					Volumes: []corev1.Volume{
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
