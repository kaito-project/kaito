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
	"slices"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// RootCauseSeverity ranks the root cause of a not-ready Workspace.
type RootCauseSeverity int

const (
	// RootCauseProgress is a Workspace still converging without an observed failure.
	RootCauseProgress RootCauseSeverity = iota
	// RootCauseFailed is a failure that may recover on its own.
	RootCauseFailed
	// RootCauseTerminal is a failure that does not recover without the user acting.
	RootCauseTerminal
)

// RootCauseStage is the stage a root cause blocks; an earlier stage blocks the later ones.
type RootCauseStage int

const (
	RootCauseStageNodes RootCauseStage = iota
	RootCauseStageModel
	RootCauseStageInference
)

// RootCause is the condition that explains why a Workspace is not ready.
type RootCause struct {
	Condition *metav1.Condition
	Severity  RootCauseSeverity
	Stage     RootCauseStage
}

// prerequisiteConditions must be True before inference can be ready; NodeClaimReady and NodesReady are checked
// because ResourceReady reports ModelMirrorNotReady while the model weights download.
var prerequisiteConditions = []kaitov1beta1.ConditionType{
	kaitov1beta1.ConditionTypeResourceStatus,
	kaitov1beta1.ConditionTypeNodeClaimStatus,
	kaitov1beta1.ConditionTypeNodeStatus,
	kaitov1beta1.WorkspaceConditionTypeModelConfigReady,
	kaitov1beta1.WorkspaceConditionTypeModelMirrorReady,
}

// terminalConditions also include InferenceReady: a terminal reason is not a consequence of a prerequisite still
// progressing, so it is the root cause whatever the other conditions report.
var terminalConditions = append(slices.Clone(prerequisiteConditions), kaitov1beta1.WorkspaceConditionTypeInferenceStatus)

// WorkspaceRootCause returns the condition that explains why the Workspace is not ready: a condition with a terminal
// reason; else a failing prerequisite; else the first prerequisite in progress; else a False InferenceReady. It
// returns false when no condition is False with a reason.
func WorkspaceRootCause(ws *kaitov1beta1.Workspace) (RootCause, bool) {
	notReady := func(condType kaitov1beta1.ConditionType) *metav1.Condition {
		cond := meta.FindStatusCondition(ws.Status.Conditions, string(condType))
		if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason == "" {
			return nil
		}
		return cond
	}
	for _, condType := range terminalConditions {
		if cond := notReady(condType); cond != nil && consts.IsTerminatedReason(cond.Reason) {
			return newRootCause(cond, RootCauseTerminal), true
		}
	}
	var progress *metav1.Condition
	for _, condType := range prerequisiteConditions {
		cond := notReady(condType)
		if cond == nil {
			continue
		}
		if !consts.IsProgressReason(cond.Reason) {
			return newRootCause(cond, RootCauseFailed), true
		}
		if progress == nil {
			progress = cond
		}
	}
	if progress != nil {
		return newRootCause(progress, RootCauseProgress), true
	}
	if cond := notReady(kaitov1beta1.WorkspaceConditionTypeInferenceStatus); cond != nil {
		if consts.IsProgressReason(cond.Reason) {
			return newRootCause(cond, RootCauseProgress), true
		}
		return newRootCause(cond, RootCauseFailed), true
	}
	return RootCause{}, false
}

func newRootCause(cond *metav1.Condition, severity RootCauseSeverity) RootCause {
	return RootCause{Condition: cond, Severity: severity, Stage: rootCauseStage(cond)}
}

func rootCauseStage(cond *metav1.Condition) RootCauseStage {
	switch kaitov1beta1.ConditionType(cond.Type) {
	case kaitov1beta1.WorkspaceConditionTypeModelConfigReady, kaitov1beta1.WorkspaceConditionTypeModelMirrorReady:
		return RootCauseStageModel
	case kaitov1beta1.WorkspaceConditionTypeInferenceStatus:
		return RootCauseStageInference
	}
	// ResourceReady reports ModelMirrorNotReady while the model weights download.
	if cond.Reason == consts.ReasonModelMirrorNotReady {
		return RootCauseStageModel
	}
	return RootCauseStageNodes
}
