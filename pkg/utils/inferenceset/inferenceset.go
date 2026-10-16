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

package inferenceset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// UpdateStatusConditionIfNotMatch updates the inferenceset status condition if it doesn't match the current values
func UpdateStatusConditionIfNotMatch(ctx context.Context, c client.Client, iObj *kaitov1beta1.InferenceSet, cType kaitov1beta1.ConditionType,
	cStatus metav1.ConditionStatus, cReason, cMessage string) error {
	if curCondition := meta.FindStatusCondition(iObj.Status.Conditions, string(cType)); curCondition != nil {
		if curCondition.Status == cStatus && curCondition.Reason == cReason && curCondition.Message == cMessage && curCondition.ObservedGeneration == iObj.GetGeneration() {
			// Nothing to change
			return nil
		}
	}
	klog.InfoS("updateStatusCondition", "inferenceset", klog.KObj(iObj), "conditionType", cType, "status", cStatus, "reason", cReason, "message", cMessage)
	condition := metav1.Condition{
		Type:               string(cType),
		Status:             cStatus,
		Reason:             cReason,
		ObservedGeneration: iObj.GetGeneration(),
		Message:            cMessage,
		LastTransitionTime: metav1.Now(),
	}
	return UpdateInferenceSetStatus(ctx, c, &client.ObjectKey{Name: iObj.Name, Namespace: iObj.Namespace}, func(status *kaitov1beta1.InferenceSetStatus) error {
		meta.SetStatusCondition(&status.Conditions, condition)
		return nil
	})
}

// UpdateInferenceSetStatus updates the inferenceset status with the provided condition
func UpdateInferenceSetStatus(ctx context.Context, c client.Client, name *client.ObjectKey, modifyFn func(*kaitov1beta1.InferenceSetStatus) error) error {
	return retry.OnError(retry.DefaultRetry,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || apierrors.IsConflict(err)
		},
		func() error {
			// Read the latest version to avoid update conflict.
			iObj := &kaitov1beta1.InferenceSet{}
			if err := c.Get(ctx, *name, iObj); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			if modifyFn != nil {
				if err := modifyFn(&iObj.Status); err != nil {
					return err
				}
			}
			return c.Status().Update(ctx, iObj)
		})
}

// UpdateInferenceSetWithRetry gets the latest inferenceset object, applies the modify function, and retries on conflict
func UpdateInferenceSetWithRetry(ctx context.Context, c client.Client, iObj *kaitov1beta1.InferenceSet, modifyFn func(*kaitov1beta1.InferenceSet) error) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestInferenceSet := &kaitov1beta1.InferenceSet{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(iObj), latestInferenceSet); err != nil {
			return err
		}
		if err := modifyFn(latestInferenceSet); err != nil {
			return err
		}
		return c.Update(ctx, latestInferenceSet)
	})
}

// ListWorkspaces lists all workspace objects in the InferenceSet's namespace that are created by the given InferenceSet.
func ListWorkspaces(ctx context.Context, iObj *kaitov1beta1.InferenceSet, kubeClient client.Client) (*kaitov1beta1.WorkspaceList, error) {
	if iObj == nil {
		return nil, fmt.Errorf("InferenceSet object is nil")
	}
	workspaceList := &kaitov1beta1.WorkspaceList{}

	// List all the workspaces in iObj.Namespace that are created by this inferenceset.
	// We use label selector to find the workspaces.
	// The label is "inferenceset.kaito.sh/created-by": <inferenceset-name>
	ls := labels.Set{
		consts.WorkspaceCreatedByInferenceSetLabel: iObj.Name,
	}

	err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		return kubeClient.List(ctx, workspaceList,
			client.InNamespace(iObj.Namespace),
			&client.MatchingLabelsSelector{Selector: ls.AsSelector()},
		)
	})
	return workspaceList, err
}

// NewWorkspaceForInferenceSet builds the child Workspace object owned by the
// given InferenceSet, applying the InferenceSet's template labels/annotations,
// owner reference, resource spec, and inference spec. The returned Workspace
// uses GenerateName so the API server assigns a unique name on creation.
//
// It is the single source of truth for child-Workspace construction: the
// InferenceSet controller and AutoUpgradeRunner create workspaces from it, and
// the InferenceSet admission webhook validates the very object it produces.
func NewWorkspaceForInferenceSet(iObj *kaitov1beta1.InferenceSet) *kaitov1beta1.Workspace {
	annotations := maps.Clone(iObj.Spec.Template.Annotations)
	if runtime := iObj.GetAnnotations()[kaitov1beta1.AnnotationWorkspaceRuntime]; runtime != "" {
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[kaitov1beta1.AnnotationWorkspaceRuntime] = runtime
	}
	// Benchmark is on by default; only propagate the opt-out when explicitly disabled.
	if !kaitov1beta1.IsInferenceSetBenchmarkEnabled(iObj) {
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[kaitov1beta1.AnnotationDisableBenchmark] = "true"
	}

	// Start with labels from the template metadata, then add controller labels.
	workspaceLabels := maps.Clone(iObj.Spec.Template.Labels)
	if workspaceLabels == nil {
		workspaceLabels = make(map[string]string)
	}
	// Also propagate select labels from the InferenceSet's own metadata,
	// in case template.metadata.labels was pruned by the API server.
	if role, ok := iObj.Labels[kaitov1beta1.LabelInferenceRole]; ok {
		workspaceLabels[kaitov1beta1.LabelInferenceRole] = role
	}
	if mriParent, ok := iObj.Labels[kaitov1alpha1.LabelMultiRoleInferenceParent]; ok {
		workspaceLabels[kaitov1alpha1.LabelMultiRoleInferenceParent] = mriParent
	}
	// Children must satisfy spec.labelSelector, so its matchLabels always win.
	if iObj.Spec.Selector != nil {
		maps.Copy(workspaceLabels, iObj.Spec.Selector.MatchLabels)
	}
	workspaceLabels[consts.WorkspaceCreatedByInferenceSetLabel] = iObj.Name

	workspaceObj := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: workspaceGenerateName(iObj.Name),
			Namespace:    iObj.Namespace,
			Labels:       workspaceLabels,
			Annotations:  annotations,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(iObj, kaitov1beta1.GroupVersion.WithKind("InferenceSet")),
			},
		},
		Resource: kaitov1beta1.ResourceSpec{
			InstanceType:  iObj.Spec.Template.Resource.InstanceType,
			LabelSelector: workspaceNodeSelector(iObj),
			Partition:     iObj.Spec.Template.Resource.Partition,
		},
		Inference: iObj.Spec.Template.Inference.DeepCopy(),
	}
	return workspaceObj
}

// generatedNameSuffixLength is the number of random characters the API server
// appends to a GenerateName prefix.
const generatedNameSuffixLength = 5

// workspaceGenerateName returns the GenerateName prefix for the child
// Workspaces of the named InferenceSet. The InferenceSet name is truncated so
// generated Workspace names stay within kaitov1beta1.MaxWorkspaceNameLength;
// the full InferenceSet name remains on the
// consts.WorkspaceCreatedByInferenceSetLabel label.
func workspaceGenerateName(inferenceSetName string) string {
	maxPrefixLength := kaitov1beta1.MaxWorkspaceNameLength - generatedNameSuffixLength - 1 // 1 for the dash separator
	if len(inferenceSetName) > maxPrefixLength {
		inferenceSetName = strings.TrimRight(inferenceSetName[:maxPrefixLength], "-")
	}
	return inferenceSetName + "-"
}

// workspaceNodeSelector returns the GPU node selector for child Workspaces:
// template.resource.labelSelector, or, for backward compatibility with node
// auto-provisioning only, spec.labelSelector when the former is unset or empty.
func workspaceNodeSelector(iObj *kaitov1beta1.InferenceSet) *metav1.LabelSelector {
	selector := iObj.Spec.Template.Resource.LabelSelector
	if kaitov1beta1.IsEmptyLabelSelector(selector) && !featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		return iObj.Spec.Selector
	}
	return selector
}

// ValidateWorkspaceForInferenceSet validates the child Workspace the given
// InferenceSet would create, with the same rules the Workspace webhook applies.
// Like any knative Validate, it treats the admission baseline in ctx as the
// previous InferenceSet: on create it applies the Workspace create-time
// checks; on update it applies the Workspace update-time checks against the
// Workspace projected from the baseline. Returned errors are rooted at the
// InferenceSet. It is wired into api/v1beta1 at startup.
func ValidateWorkspaceForInferenceSet(ctx context.Context, iObj *kaitov1beta1.InferenceSet) *apis.FieldError {
	ws := NewWorkspaceForInferenceSet(iObj)
	base, _ := apis.GetBaseline(ctx).(*kaitov1beta1.InferenceSet)
	if base == nil {
		return ws.ValidateCreate(ctx).ViaField("spec", "template")
	}

	oldWs := NewWorkspaceForInferenceSet(base)
	// When both node selectors are inherited from spec.labelSelector, report a
	// change only at spec.labelSelector instead of the projected template path.
	if kaitov1beta1.IsEmptyLabelSelector(iObj.Spec.Template.Resource.LabelSelector) &&
		kaitov1beta1.IsEmptyLabelSelector(base.Spec.Template.Resource.LabelSelector) {
		oldWs.Resource.LabelSelector = ws.Resource.LabelSelector
	}
	var errs *apis.FieldError
	if !apiequality.Semantic.DeepEqual(iObj.Spec.Selector, base.Spec.Selector) {
		errs = kaitov1beta1.ValidateLabelSelectorUpdate(iObj.Spec.Selector, base.Spec.Selector).ViaField("spec")
	}

	templateErrs := ws.ValidateUpdate(ctx, oldWs)
	if !apiequality.Semantic.DeepEqual(iObj.Spec.Template, base.Spec.Template) {
		// Replicas created after this update use the new template, so it must
		// also pass the checks a newly created Workspace would face.
		templateErrs = templateErrs.Also(ws.ValidateCreate(ctx))
	}
	return errs.Also(templateErrs.ViaField("spec", "template"))
}

func ComputeInferenceSetHash(iObj *kaitov1beta1.InferenceSet) string {
	if iObj == nil {
		return ""
	}

	hasher := sha256.New()
	encoder := json.NewEncoder(hasher)
	payload := map[string]interface{}{
		"spec":              iObj.Spec,
		"runtimeAnnotation": iObj.GetAnnotations()[kaitov1beta1.AnnotationWorkspaceRuntime],
	}
	encoder.Encode(payload)
	return hex.EncodeToString(hasher.Sum(nil))
}

func MarshalInferenceSetFields(iObj *kaitov1beta1.InferenceSet) ([]byte, error) {
	if iObj == nil {
		return nil, fmt.Errorf("InferenceSet object is nil")
	}

	partialMap := map[string]interface{}{
		"Spec": iObj.Spec,
	}

	jsonData, err := json.Marshal(partialMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal selected fields: %w", err)
	}

	return jsonData, nil
}
