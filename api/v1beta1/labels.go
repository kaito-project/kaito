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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

const (

	// Non-prefixed labels/annotations are reserved for end-use.

	// KAITOPrefix Kubernetes Data Mining prefix.
	KAITOPrefix = "kaito.sh/"

	// AnnotationEnableLB determines whether KAITO creates LoadBalancer type service for testing.
	AnnotationEnableLB = KAITOPrefix + "enablelb"

	// LabelWorkspaceName is the label for workspace name.
	LabelWorkspaceName = KAITOPrefix + "workspace"

	// LabelRAGEngineName is the label for ragengine name.
	LabelRAGEngineName = KAITOPrefix + "ragengine"

	// LabelWorkspaceName is the label for workspace namespace.
	LabelWorkspaceNamespace = KAITOPrefix + "workspacenamespace"

	// LabelRAGEngineNamespace is the label for ragengine namespace.
	LabelRAGEngineNamespace = KAITOPrefix + "ragenginenamespace"

	// WorkspaceRevisionAnnotation is the Annotations for revision number
	WorkspaceRevisionAnnotation = "workspace.kaito.io/revision"

	// RAGEngineRevisionAnnotation is the Annotations for revision number
	RAGEngineRevisionAnnotation = "ragengine.kaito.io/revision"

	// AnnotationWorkspaceRuntime is the annotation for runtime selection.
	AnnotationWorkspaceRuntime = KAITOPrefix + "runtime"

	// AnnotationBypassResourceChecks allows bypassing resource requirement checks like GPU memory.
	AnnotationBypassResourceChecks = KAITOPrefix + "bypass-resource-checks"

	// AnnotationNodeImageFamily specifies node image family used by generated NodeClaim.
	AnnotationNodeImageFamily = KAITOPrefix + "node-image-family"

	// AnnotationNodeClassName specifies the Karpenter NodeClass name to use.
	// When set on a Workspace, the karpenter provisioner uses this value directly
	// as the NodeClassRef name instead of the configured default.
	AnnotationNodeClassName = KAITOPrefix + "node-class-name"

	// AnnotationDisableBenchmark disables the post-load throughput benchmark stage.
	// The benchmark is enabled by default. Set to "true" on a Workspace to
	// disable it; when absent or any other value, the benchmark runs.
	AnnotationDisableBenchmark = KAITOPrefix + "disable-benchmark"

	// InferenceSetRevisionAnnotation is the Annotations for revision number
	InferenceSetRevisionAnnotation = "inferenceset.kaito.io/revision"

	// LabelInferenceRole indicates the inference role of a workspace in P/D disaggregated serving.
	// Propagated from InferenceSet.Spec.Template.Metadata.Labels onto child workspaces by the InferenceSet controller.
	// Valid values: "prefill", "decode".
	LabelInferenceRole = KAITOPrefix + "inference-role"

	// InferenceRoleDecode is the decode role value for token generation in P/D disaggregated serving.
	InferenceRoleDecode = "decode"

	// AnnotationPerformanceMode selects the vLLM performance preset.
	// Valid values are "balanced" (default), "interactivity", and "throughput".
	//   - "interactivity": optimizes for low per-request latency (fine-grained CUDA
	//     graphs, latency-oriented kernels, smaller batch sizes).
	//   - "balanced": sensible trade-off between latency and throughput (default).
	//   - "throughput": maximizes aggregate tokens/sec (larger CUDA graphs, more
	//     aggressive batching, throughput-oriented kernels).
	// Only supported when the vLLM runtime is used.
	AnnotationPerformanceMode = KAITOPrefix + "performance-mode"
)

// Valid values for AnnotationPerformanceMode.
const (
	PerformanceModeBalanced      = "balanced"
	PerformanceModeInteractivity = "interactivity"
	PerformanceModeThroughput    = "throughput"
)

// GetWorkspaceRuntimeName returns the runtime name of the workspace.
func GetWorkspaceRuntimeName(ws *Workspace) model.RuntimeName {
	if ws == nil {
		panic("workspace is nil")
	}

	if !featuregates.FeatureGates[consts.FeatureFlagVLLM] {
		return model.RuntimeNameHuggingfaceTransformers
	}

	runtime := model.RuntimeNameVLLM
	name := ws.Annotations[AnnotationWorkspaceRuntime]
	switch name {
	case string(model.RuntimeNameHuggingfaceTransformers):
		runtime = model.RuntimeNameHuggingfaceTransformers
	case string(model.RuntimeNameVLLM):
		runtime = model.RuntimeNameVLLM
	}

	return runtime
}

// IsRunBenchmarkEnabled reports whether the workspace benchmark is enabled.
// The benchmark is on by default; it is only disabled when the annotation
// kaito.sh/disable-benchmark is explicitly set to "true".
func IsRunBenchmarkEnabled(ws *Workspace) bool {
	return ws.Annotations[AnnotationDisableBenchmark] != "true"
}

// ShouldRunBenchmark reports whether the workspace should run the post-load
// benchmark. The benchmark requires all of the following:
//  1. The benchmark is not disabled via annotation.
//  2. The workspace uses the vLLM runtime (benchmark_entrypoint.py is vLLM-only).
//  3. The workspace uses a preset inference config (template workspaces use
//     custom containers that do not include the benchmark entrypoint).
func ShouldRunBenchmark(ws *Workspace) bool {
	return IsRunBenchmarkEnabled(ws) &&
		GetWorkspaceRuntimeName(ws) == model.RuntimeNameVLLM &&
		ws.Inference != nil && ws.Inference.Preset != nil
}

// GetPerformanceMode returns the performance mode annotation value, defaulting to
// PerformanceModeBalanced when the annotation is absent or empty.
func GetPerformanceMode(ws *Workspace) string {
	if ws == nil {
		return PerformanceModeBalanced
	}
	if v := ws.Annotations[AnnotationPerformanceMode]; v != "" {
		return v
	}
	return PerformanceModeBalanced
}

// reservedSelectorLabelKeys are labels that KAITO controllers apply to their
// own NodeClaims/Nodes/NodePools. Users must not include them in resource
// label selectors; if they do, the values are silently ignored to prevent
// cross-resource targeting (e.g. matching another Workspace's nodes).
var reservedSelectorLabelKeys = map[string]struct{}{
	// KAITO workspace/ragengine identity labels.
	LabelWorkspaceName:      {},
	LabelRAGEngineName:      {},
	LabelWorkspaceNamespace: {},
	LabelRAGEngineNamespace: {},

	// Karpenter NodePool management labels.
	consts.KarpenterWorkspaceNameKey:         {},
	consts.KarpenterWorkspaceNamespaceKey:    {},
	consts.KarpenterInferenceSetKey:          {},
	consts.KarpenterInferenceSetNamespaceKey: {},
}

// IsReservedSelectorLabel reports whether the given label key is reserved by
// KAITO and must not be honored when supplied via a user-defined selector.
func IsReservedSelectorLabel(key string) bool {
	_, ok := reservedSelectorLabelKeys[key]
	return ok
}

// SanitizedMatchLabels returns the MatchLabels of selector with any
// KAITO-reserved keys removed. Returns nil when selector is nil or has no
// non-reserved entries. The returned map is always a fresh copy when at
// least one entry is preserved; callers must not assume identity with the
// input map.
func SanitizedMatchLabels(selector *metav1.LabelSelector) map[string]string {
	if selector == nil || len(selector.MatchLabels) == 0 {
		return nil
	}
	out := make(map[string]string, len(selector.MatchLabels))
	for k, v := range selector.MatchLabels {
		if IsReservedSelectorLabel(k) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// GetInferenceSetRuntimeName returns the runtime name for an InferenceSet.
func GetInferenceSetRuntimeName(iObj *InferenceSet) model.RuntimeName {
	if iObj == nil {
		panic("inferenceset is nil")
	}

	if !featuregates.FeatureGates[consts.FeatureFlagVLLM] {
		return model.RuntimeNameHuggingfaceTransformers
	}

	runtime := model.RuntimeNameVLLM
	name := iObj.Annotations[AnnotationWorkspaceRuntime]
	switch name {
	case string(model.RuntimeNameHuggingfaceTransformers):
		runtime = model.RuntimeNameHuggingfaceTransformers
	case string(model.RuntimeNameVLLM):
		runtime = model.RuntimeNameVLLM
	}

	return runtime
}

// IsInferenceSetBenchmarkEnabled reports whether the InferenceSet benchmark is enabled.
func IsInferenceSetBenchmarkEnabled(iObj *InferenceSet) bool {
	return iObj.Annotations[AnnotationDisableBenchmark] != "true"
}

// ShouldRunInferenceSetBenchmark reports whether the InferenceSet's child workspaces should
// run the post-load benchmark.
func ShouldRunInferenceSetBenchmark(iObj *InferenceSet) bool {
	return IsInferenceSetBenchmarkEnabled(iObj) &&
		GetInferenceSetRuntimeName(iObj) == model.RuntimeNameVLLM &&
		iObj.Spec.Template.Inference.Preset != nil
}
