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

// Node provisioning failure reasons on the Workspace NodeClaimReady and ResourceReady conditions that need the user to
// act, such as raising a quota or fixing the node configuration. The message starts with the reason reported by the
// node provisioner, so these stay stable across provisioner versions.
const (
	// ReasonQuotaExceeded reports an exhausted subscription vCPU quota (VM SKU family, regional or spot).
	ReasonQuotaExceeded = "QuotaExceeded"
	// ReasonCapacityUnavailable reports that the cloud cannot allocate the VM size, or does not offer it.
	ReasonCapacityUnavailable = "CapacityUnavailable"
	// ReasonNodeImageInvalid reports a node image that does not exist or is not a valid image reference, or a NodeClass
	// with no image compatible with the instance type.
	ReasonNodeImageInvalid = "NodeImageInvalid"
	// ReasonNodeBootstrapFailed reports a node that failed its bootstrap script or never registered with the cluster.
	ReasonNodeBootstrapFailed = "NodeBootstrapFailed"
	// ReasonNodeConfigurationInvalid reports a permission, subnet, Azure Policy, resource provider registration or
	// referenced resource that prevents creating the node.
	ReasonNodeConfigurationInvalid = "NodeConfigurationInvalid"
	// ReasonNodeClassInvalid reports a NodeClass that is missing or Ready=False, which blocks every launch. It is read
	// on every reconcile, so it is reported only while it lasts.
	ReasonNodeClassInvalid = "NodeClassInvalid"
	// ReasonNodePoolInvalid reports a NodePool rejected by Karpenter's validation, which blocks every launch. It is read
	// on every reconcile, so it is reported only while it lasts.
	ReasonNodePoolInvalid = "NodePoolInvalid"
)

// Inference failure reasons on the Workspace InferenceReady condition that need the user to act, such as changing the
// instance type, the model image or the model configuration.
const (
	// ReasonContainerOOMKilled reports an inference container killed for exceeding its memory.
	ReasonContainerOOMKilled = "ContainerOOMKilled"
	// ReasonImagePullError reports an inference image that cannot be pulled.
	ReasonImagePullError = "ImagePullError"
	// ReasonContainerStartError reports an inference container that cannot be created or started, e.g. a missing Secret.
	ReasonContainerStartError = "ContainerStartError"
	// ReasonContainerCrashLoopBackOff reports an inference container that keeps crashing.
	ReasonContainerCrashLoopBackOff = "ContainerCrashLoopBackOff"
	// ReasonSASTokenFetchFailed reports a model streaming init container that cannot obtain a SAS token.
	ReasonSASTokenFetchFailed = "SASTokenFetchFailed"
	// ReasonModelBundleMismatch reports a streamed model bundle that does not match the deployment's configuration.
	ReasonModelBundleMismatch = "ModelBundleMismatch"
	// ReasonModelStreamingStorageClassNotFound reports a missing model streaming StorageClass.
	ReasonModelStreamingStorageClassNotFound = "ModelStreamingStorageClassNotFound"
	// ReasonModelStreamingInvalidStorageClass reports a model streaming StorageClass that cannot be used.
	ReasonModelStreamingInvalidStorageClass = "ModelStreamingInvalidStorageClass"
	// ReasonModelStreamingServiceAccountInvalid reports an invalid model streaming ServiceAccount.
	ReasonModelStreamingServiceAccountInvalid = "ModelStreamingServiceAccountInvalid"
	// ReasonModelStreamingInvalidAnnotations reports invalid model streaming annotations.
	ReasonModelStreamingInvalidAnnotations = "ModelStreamingInvalidAnnotations"
)

// ReasonDownloadOOMKilled on the Workspace ModelMirrorReady condition reports a model download killed for exceeding
// its memory.
const ReasonDownloadOOMKilled = "DownloadOOMKilled"

// In-progress reasons of a not-ready Workspace condition: the Workspace is still converging, without an observed
// failure. Any other False reason is a failure. A controller that reports a new in-progress reason must add it here,
// or an InferenceSet reports it as a failure of its replica.
const (
	// ReasonResourceNotReady is reported on ResourceReady while NodeClaimReady or NodesReady is not ready.
	ReasonResourceNotReady = "workspaceResourceStatusNotReady"
	// ReasonAwaitingReconciliation is the state Karpenter stamps on a NodeClaim condition it has not reconciled yet.
	ReasonAwaitingReconciliation = "AwaitingReconciliation"
	// ReasonNodeClaimNotReady is reported on NodeClaimReady while NodeClaims are provisioning without an observed
	// failure.
	ReasonNodeClaimNotReady = "NodeClaimNotReady"
	// ReasonNodeNotReady is reported on NodesReady while not enough Nodes are ready.
	ReasonNodeNotReady = "NodeNotReady"
	// ReasonNodePluginsNotReady is reported on NodesReady while the GPU device plugins are not ready.
	ReasonNodePluginsNotReady = "NodePluginsNotReady"
	// ReasonModelMirrorNotReady is reported on ResourceReady while the model weights download.
	ReasonModelMirrorNotReady = "ModelMirrorNotReady"
	// ReasonModelMirrorPending is reported on ModelMirrorReady while the download has not reported a result.
	ReasonModelMirrorPending = "ModelMirrorPending"
	// ReasonModelMirrorDeleting is reported on ModelMirrorReady while the ModelMirror is deleted.
	ReasonModelMirrorDeleting = "ModelMirrorDeleting"
	// ReasonInferencePending is reported on InferenceReady while the inference workload is not ready.
	ReasonInferencePending = "WorkspaceInferenceStatusPending"
)

// IsProgressReason reports whether a not-ready Workspace condition reason is in progress rather than a failure.
func IsProgressReason(reason string) bool {
	switch reason {
	case ReasonResourceNotReady,
		ReasonAwaitingReconciliation,
		ReasonNodeClaimNotReady,
		ReasonNodeNotReady,
		ReasonNodePluginsNotReady,
		ReasonModelMirrorNotReady,
		ReasonModelMirrorPending,
		ReasonModelMirrorDeleting,
		ReasonInferencePending:
		return true
	}
	return false
}

// IsTerminatedReason reports whether a not-ready Workspace or InferenceSet reason cannot recover without the user
// acting: the controllers keep retrying, but the same failure recurs until the user changes the configuration or the
// cloud subscription. An InferenceSet with a replica failing for one of these reasons reports it on its
// InferenceSetReady condition, so callers can pass the reason of an InferenceSetReady=False condition whose
// observedGeneration matches the InferenceSet's generation.
func IsTerminatedReason(reason string) bool {
	switch reason {
	case ReasonQuotaExceeded,
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
		ReasonDownloadOOMKilled:
		return true
	}
	return false
}
