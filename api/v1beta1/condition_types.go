// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package v1beta1

// ConditionType is a valid value for Condition.Type.
type ConditionType string

const (
	// ConditionTypeNodeClaimStatus is the state when checking nodeClaim status.
	ConditionTypeNodeClaimStatus = ConditionType("NodeClaimReady")

	// ConditionTypeResourceStatus is the state when Resource has been created.
	ConditionTypeResourceStatus = ConditionType("ResourceReady")

	// WorkspaceConditionTypeInferenceStatus is the state when Inference service has been ready.
	WorkspaceConditionTypeInferenceStatus = ConditionType("InferenceReady")

	// WorkspaceConditionTypeTuningJobStatus is the state when the tuning job starts normally.
	WorkspaceConditionTypeTuningJobStatus ConditionType = ConditionType("JobStarted")

	//WorkspaceConditionTypeDeleting is the Workspace state when starts to get deleted.
	WorkspaceConditionTypeDeleting = ConditionType("WorkspaceDeleting")

	//WorkspaceConditionTypeSucceeded is the Workspace state that summarizes all operations' states.
	//For inference, the "True" condition means the inference service is ready to serve requests.
	//For fine tuning, the "True" condition means the tuning job completes successfully.
	WorkspaceConditionTypeSucceeded ConditionType = ConditionType("WorkspaceSucceeded")

	// RAGEngineConditionTypeServiceStatus is the state when service has been ready.
	RAGEngineConditionTypeServiceStatus = ConditionType("ServiceReady")

	// RAGConditionTypeServiceStatus is the state when RAG Engine service has been ready.
	RAGConditionTypeServiceStatus = ConditionType("RAGEngineServiceReady")

	//RAGEngineConditionTypeDeleting is the RAGEngine state when starts to get deleted.
	RAGEngineConditionTypeDeleting = ConditionType("RAGEngineDeleting")

	// RAGEngineConditionTypeSucceeded is the RAGEngine state that summarizes all operations' states.
	RAGEngineConditionTypeSucceeded ConditionType = ConditionType("RAGEngineSucceeded")
)
