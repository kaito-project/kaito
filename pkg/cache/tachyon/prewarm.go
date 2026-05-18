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

package tachyon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/cache"
)

const (
	// PrewarmJobLabelKey identifies Jobs created by the cache prewarm system.
	PrewarmJobLabelKey = "kaito.sh/cache-prewarm"
	// PrewarmJobLabelValue is the constant label value for prewarm Jobs.
	PrewarmJobLabelValue = "true"
	// PrewarmModelLabel records which model a prewarm Job targets.
	PrewarmModelLabel = "kaito.sh/cache-prewarm-model"
	// PrewarmHashLabel records a hash of the prewarm parameters for idempotency.
	PrewarmHashLabel = "kaito.sh/cache-prewarm-hash"

	// Default backoff limit for prewarm Jobs.
	defaultBackoffLimit = int32(3)
	// TTL after completion (1 hour).
	defaultTTLSeconds = int32(3600)
)

// BuildPrewarmJob constructs a Kubernetes Job spec for prewarming a model
// into the Tachyon cache. The Job downloads model weights from HuggingFace
// and uploads them to the Tachyon cache using the Python client, which
// automatically replicates to blob storage.
func (p *Provider) BuildPrewarmJob(req cache.PrewarmRequest, namespace string) *batchv1.Job {
	blobPath := ModelBlobRelativePath(p.config.BlobPrefix, req.ModelName, req.ModelRevision)
	jobName := prewarmJobName(namespace, req.ModelName, req.ModelRevision)
	hash := prewarmHash(req.ModelName, req.ModelRevision, p.config.BlobEndpoint, p.config.BlobContainer)

	// Sanitize model name for label value (must be <= 63 chars, valid label).
	modelLabel := sanitizeLabelValue(req.ModelName)

	backoffLimit := defaultBackoffLimit
	ttl := defaultTTLSeconds

	env := []corev1.EnvVar{
		{Name: "MODEL_ID", Value: req.ModelName},
		{Name: "MODEL_REVISION", Value: req.ModelRevision},
		{Name: "BLOB_ENDPOINT", Value: p.config.BlobEndpoint},
		{Name: "BLOB_CONTAINER", Value: p.config.BlobContainer},
		{Name: "BLOB_PATH", Value: blobPath},
		{Name: "TACHYON_DISCOVERY_ENDPOINT", Value: p.config.DiscoveryEndpoint},
	}

	// If an HF token secret is specified, mount it as an env var.
	if req.ModelAccessSecret != "" {
		env = append(env, corev1.EnvVar{
			Name: "HF_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: req.ModelAccessSecret},
					Key:                  "HF_TOKEN",
					Optional:             boolPtr(true),
				},
			},
		})
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				PrewarmJobLabelKey: PrewarmJobLabelValue,
				PrewarmModelLabel:  modelLabel,
				PrewarmHashLabel:   hash,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						PrewarmJobLabelKey: PrewarmJobLabelValue,
						PrewarmModelLabel:  modelLabel,
						InjectLabelKey:     InjectLabelValue,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:  "prewarm",
							Image: p.config.PrewarmImage,
							Env:   env,
						},
					},
				},
			},
		},
	}

	return job
}

// prewarmJobName generates a deterministic Job name from model parameters.
// Includes a hash suffix for uniqueness across revisions and config changes.
func prewarmJobName(namespace, modelID, revision string) string {
	// Create a short hash for uniqueness.
	h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s", namespace, modelID, revision)))
	hashSuffix := hex.EncodeToString(h[:4]) // 8 hex chars

	// Sanitize model name: replace / with - and truncate.
	name := strings.ReplaceAll(modelID, "/", "-")
	name = strings.ToLower(name)

	// Job names must be <= 63 chars. Reserve space for prefix + hash suffix.
	const maxNameLen = 63
	prefix := "cache-prewarm-"
	suffix := "-" + hashSuffix
	maxModelLen := maxNameLen - len(prefix) - len(suffix)
	if len(name) > maxModelLen {
		name = name[:maxModelLen]
	}

	return prefix + name + suffix
}

// prewarmHash creates a short hash of prewarm parameters for idempotency checks.
func prewarmHash(modelID, revision, blobEndpoint, blobContainer string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s", modelID, revision, blobEndpoint, blobContainer)))
	return hex.EncodeToString(h[:8])
}

// sanitizeLabelValue ensures a string is a valid Kubernetes label value.
func sanitizeLabelValue(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ToLower(s)
	if len(s) > 63 {
		s = s[:63]
	}
	return s
}

func boolPtr(b bool) *bool { return &b }
