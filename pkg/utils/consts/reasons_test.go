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

import "testing"

func TestIsTerminatedReason(t *testing.T) {
	terminated := []string{
		ReasonQuotaExceeded,
		ReasonCapacityUnavailable,
		ReasonNodeImageInvalid,
		ReasonNodeBootstrapFailed,
		ReasonNodeConfigurationInvalid,
		ReasonNodeClassInvalid,
		ReasonNodePoolInvalid,
		ReasonContainerOOMKilled,
		ReasonImagePullError,
		ReasonContainerStartError,
		ReasonContainerCrashLoopBackOff,
		ReasonSASTokenFetchFailed,
		ReasonModelBundleMismatch,
		ReasonModelStreamingStorageClassNotFound,
		ReasonModelStreamingInvalidStorageClass,
		ReasonModelStreamingServiceAccountInvalid,
		ReasonModelStreamingInvalidAnnotations,
		ReasonDownloadOOMKilled,
	}
	for _, reason := range terminated {
		if !IsTerminatedReason(reason) {
			t.Errorf("IsTerminatedReason(%q) = false, want true", reason)
		}
	}

	// Progress and failures that can recover, as reported on Workspace and InferenceSet conditions.
	notTerminated := []string{
		"",
		"NodeClaimNotReady",
		"InsufficientCapacityError",
		"CreateInstanceFailed",
		"Unschedulable",
		"PodEvicted",
		"WorkspaceInferenceStatusPending",
		"ModelMirrorPending",
		"inferencesetNotReady",
		"quotaexceeded",
		"NodeClassNotReady",
		"NodePoolValidationFailed",
	}
	for _, reason := range notTerminated {
		if IsTerminatedReason(reason) {
			t.Errorf("IsTerminatedReason(%q) = true, want false", reason)
		}
	}
}

func TestIsProgressReason(t *testing.T) {
	progress := []string{
		"workspaceResourceStatusNotReady",
		"AwaitingReconciliation",
		"NodeClaimNotReady",
		"NodeNotReady",
		"NodePluginsNotReady",
		"ModelMirrorNotReady",
		"ModelMirrorPending",
		"ModelMirrorDeleting",
		"WorkspaceInferenceStatusPending",
	}
	for _, reason := range progress {
		if !IsProgressReason(reason) {
			t.Errorf("IsProgressReason(%q) = false, want true", reason)
		}
	}

	notProgress := []string{"", ReasonQuotaExceeded, "CreateInstanceFailed", "Unschedulable", "NodeClaimsReady", "nodeclaimnotready"}
	for _, reason := range notProgress {
		if IsProgressReason(reason) {
			t.Errorf("IsProgressReason(%q) = true, want false", reason)
		}
	}
}
