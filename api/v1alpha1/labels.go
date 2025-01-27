// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

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
