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

package inference

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

// StreamingDefaults holds the cluster-wide defaults for model streaming,
// set once at startup from controller flags.
var StreamingDefaults = struct {
	StorageClass   string
	ServiceAccount string
	ModelStreamer  ModelStreamer
}{}

// ModelStreamingEnabled returns true if the ModelStreaming feature gate is on
// AND the workspace does not have the opt-out annotation.
func ModelStreamingEnabled(ws *v1beta1.Workspace) bool {
	if !featuregates.FeatureGates[consts.FeatureFlagModelStreaming] {
		return false
	}
	if ann := ws.Annotations[mmconsts.AnnotationModelStreaming]; ann == "disabled" {
		return false
	}
	if v1beta1.GetWorkspaceRuntimeName(ws) != pkgmodel.RuntimeNameVLLM {
		return false
	}
	return true
}

// sha256First6 returns the first 6 hex characters of the SHA-256 hash of the input.
func sha256First6(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])[:6]
}

// ModelMirrorCRName derives the ModelMirror CR name from a HuggingFace model ID.
func ModelMirrorCRName(modelID string) string {
	return sha256First6(modelID)
}

// ResolveHFModelID resolves the HuggingFace model ID from a workspace's preset name.
// Returns "" if the workspace has no inference preset.
func ResolveHFModelID(ws *v1beta1.Workspace) string {
	if ws.Inference == nil || ws.Inference.Preset == nil {
		return ""
	}
	return plugin.ResolveHFModelID(string(ws.Inference.Preset.Name))
}

// ResolveStreamingServiceAccount resolves the ServiceAccount name for streaming.
// Priority: workspace annotation > controller flag > error.
func ResolveStreamingServiceAccount(ws *v1beta1.Workspace, defaultSA string) (string, error) {
	saName := ws.Annotations[mmconsts.AnnotationStreamingServiceAccount]
	if saName == "" {
		saName = defaultSA
	}
	if saName == "" {
		return "", fmt.Errorf("model streaming enabled but no service account configured: "+
			"set annotation %s on the workspace or --default-streaming-service-account on the controller",
			mmconsts.AnnotationStreamingServiceAccount)
	}
	return saName, nil
}

// ResolveStorageClass resolves the StorageClass for the ModelMirror PVC.
// Priority: workspace annotation > controller flag.
func ResolveStorageClass(ws *v1beta1.Workspace, defaultSC string) string {
	sc := ws.Annotations[mmconsts.AnnotationModelMirrorStorageClass]
	if sc == "" {
		sc = defaultSC
	}
	return sc
}

// buildCommonStreamingEnvVars returns env vars common to all cloud providers:
// KAITO_PROCESSOR (for benchmark probe). HF_TOKEN is handled by SetHFToken.
func buildCommonStreamingEnvVars(modelID string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "KAITO_PROCESSOR", Value: modelID},
	}
}

// SetStreamingConfig modifies the pod spec for streaming mode:
// - Adds provider-specific env vars (e.g. AZURE_STORAGE_ACCOUNT_NAME)
// - Adds common env vars (e.g. KAITO_PROCESSOR)
// - Sets serviceAccountName
//
// Note: weights volume mount removal and init container skipping are handled upstream —
// GenerateInferencePodSpec skips the mount when streamingModelPath is set, and
// SetModelDownloadInfo returns early when streaming is enabled.
func SetStreamingConfig(streamingCfg *StreamingConfig, modelID, defaultSA string) func(*generator.WorkspaceGeneratorContext, *corev1.PodSpec) error {
	return func(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
		found := false
		for i := range spec.Containers {
			if spec.Containers[i].Name == ctx.Workspace.Name {
				// Add provider-specific env vars (e.g. AZURE_STORAGE_ACCOUNT_NAME)
				spec.Containers[i].Env = append(spec.Containers[i].Env, streamingCfg.ProviderEnvVars...)

				// Add common streaming env vars
				spec.Containers[i].Env = append(spec.Containers[i].Env, buildCommonStreamingEnvVars(modelID)...)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("inference container %q not found in pod spec", ctx.Workspace.Name)
		}

		// Set ServiceAccount (defaultSA is the controller flag value)
		saName, err := ResolveStreamingServiceAccount(ctx.Workspace, defaultSA)
		if err != nil {
			return err
		}
		spec.ServiceAccountName = saName
		return nil
	}
}
