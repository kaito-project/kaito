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
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

const (

	// Non-prefixed labels/annotations are reserved for end-use.

	// KAITOPrefix Kubernetes Data Mining prefix.
	KAITOPrefix = "kaito.sh/"

	// AnnotationEnableLB determines whether kaito creates LoadBalancer type service for testing.
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
