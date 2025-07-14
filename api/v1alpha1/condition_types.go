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

// ConditionType is a valid value for Condition.Type.
type ConditionType string

const (
	// ConditionTypeNodeClaimStatus is the state when checking nodeClaim status.
	ConditionTypeNodeClaimStatus = ConditionType("NodeClaimReady")

	// ConditionTypeResourceStatus is the state when Resource has been created.
	ConditionTypeResourceStatus = ConditionType("ResourceReady")

	// WorkspaceConditionTypeInferenceStatus is the state when Inference service has been ready.
	WorkspaceConditionTypeInferenceStatus = ConditionType("InferenceReady")

	// RAGEneineConditionTypeServiceStatus is the state when service has been ready.
	RAGEneineConditionTypeServiceStatus = ConditionType("ServiceReady")

	// RAGConditionTypeServiceStatus is the state when RAG Engine service has been ready.
	RAGConditionTypeServiceStatus = ConditionType("RAGEngineServiceReady")

	// WorkspaceConditionTypeTuningJobStatus is the state when the tuning job starts normally.
	WorkspaceConditionTypeTuningJobStatus ConditionType = ConditionType("JobStarted")

	//RAGEngineConditionTypeDeleting is the RAGEngine state when starts to get deleted.
	RAGEngineConditionTypeDeleting = ConditionType("RAGEngineDeleting")

	//WorkspaceConditionTypeDeleting is the Workspace state when starts to get deleted.
	WorkspaceConditionTypeDeleting = ConditionType("WorkspaceDeleting")

	//WorkspaceConditionTypeSucceeded is the Workspace state that summarizes all operations' states.
	//For inference, the "True" condition means the inference service is ready to serve requests.
	//For fine tuning, the "True" condition means the tuning job completes successfully.
	WorkspaceConditionTypeSucceeded ConditionType = ConditionType("WorkspaceSucceeded")

	RAGEngineConditionTypeSucceeded ConditionType = ConditionType("RAGEngineSucceeded")
)
