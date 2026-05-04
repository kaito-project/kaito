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
