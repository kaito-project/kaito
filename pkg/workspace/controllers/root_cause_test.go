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

package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestWorkspaceRootCause(t *testing.T) {
	falseCond := func(condType kaitov1beta1.ConditionType, reason string) metav1.Condition {
		return metav1.Condition{Type: string(condType), Status: metav1.ConditionFalse, Reason: reason}
	}
	testcases := map[string]struct {
		conditions       []metav1.Condition
		expectedFound    bool
		expectedType     kaitov1beta1.ConditionType
		expectedSeverity RootCauseSeverity
		expectedStage    RootCauseStage
	}{
		"no False condition": {
			conditions: []metav1.Condition{{Type: string(kaitov1beta1.ConditionTypeResourceStatus), Status: metav1.ConditionTrue, Reason: "ok"}},
		},
		"terminal InferenceReady wins over a prerequisite in progress": {
			conditions: []metav1.Condition{
				falseCond(kaitov1beta1.ConditionTypeNodeStatus, consts.ReasonNodeNotReady),
				falseCond(kaitov1beta1.WorkspaceConditionTypeInferenceStatus, consts.ReasonContainerOOMKilled),
			},
			expectedFound: true, expectedType: kaitov1beta1.WorkspaceConditionTypeInferenceStatus,
			expectedSeverity: RootCauseTerminal, expectedStage: RootCauseStageInference,
		},
		"failing prerequisite wins over an earlier one in progress": {
			conditions: []metav1.Condition{
				falseCond(kaitov1beta1.ConditionTypeResourceStatus, consts.ReasonResourceNotReady),
				falseCond(kaitov1beta1.ConditionTypeNodeClaimStatus, "CreateInstanceFailed"),
			},
			expectedFound: true, expectedType: kaitov1beta1.ConditionTypeNodeClaimStatus,
			expectedSeverity: RootCauseFailed, expectedStage: RootCauseStageNodes,
		},
		"first prerequisite in progress": {
			conditions: []metav1.Condition{
				falseCond(kaitov1beta1.ConditionTypeNodeClaimStatus, consts.ReasonNodeClaimNotReady),
				falseCond(kaitov1beta1.ConditionTypeResourceStatus, consts.ReasonResourceNotReady),
			},
			expectedFound: true, expectedType: kaitov1beta1.ConditionTypeResourceStatus,
			expectedSeverity: RootCauseProgress, expectedStage: RootCauseStageNodes,
		},
		"model weights downloading block the model stage": {
			conditions:    []metav1.Condition{falseCond(kaitov1beta1.ConditionTypeResourceStatus, consts.ReasonModelMirrorNotReady)},
			expectedFound: true, expectedType: kaitov1beta1.ConditionTypeResourceStatus,
			expectedSeverity: RootCauseProgress, expectedStage: RootCauseStageModel,
		},
		"InferenceReady in progress": {
			conditions:    []metav1.Condition{falseCond(kaitov1beta1.WorkspaceConditionTypeInferenceStatus, consts.ReasonInferencePending)},
			expectedFound: true, expectedType: kaitov1beta1.WorkspaceConditionTypeInferenceStatus,
			expectedSeverity: RootCauseProgress, expectedStage: RootCauseStageInference,
		},
		"InferenceReady failing": {
			conditions:    []metav1.Condition{falseCond(kaitov1beta1.WorkspaceConditionTypeInferenceStatus, "Unschedulable")},
			expectedFound: true, expectedType: kaitov1beta1.WorkspaceConditionTypeInferenceStatus,
			expectedSeverity: RootCauseFailed, expectedStage: RootCauseStageInference,
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ws := &kaitov1beta1.Workspace{Status: kaitov1beta1.WorkspaceStatus{Conditions: tc.conditions}}
			cause, found := WorkspaceRootCause(ws)
			assert.Equal(t, tc.expectedFound, found)
			if !tc.expectedFound {
				return
			}
			assert.Equal(t, string(tc.expectedType), cause.Condition.Type)
			assert.Equal(t, tc.expectedSeverity, cause.Severity)
			assert.Equal(t, tc.expectedStage, cause.Stage)
		})
	}
}
