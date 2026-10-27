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

package nodeclaim

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	azurev1beta1 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1beta1"
	"github.com/awslabs/operatorpkg/status"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/apis"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodes"
)

var (
	// nodeClaimStatusTimeoutInterval is the interval to check the nodeClaim status.
	nodeClaimStatusTimeoutInterval = 240 * time.Second

	WorkspaceSelector, _ = metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: kaitov1beta1.LabelWorkspaceName, Operator: metav1.LabelSelectorOpExists},
		},
	})

	KarpenterWorkspaceSelector, _ = metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: consts.KarpenterWorkspaceNameKey, Operator: metav1.LabelSelectorOpExists},
		},
	})

	RagEngineSelector, _ = metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: kaitov1beta1.LabelRAGEngineName, Operator: metav1.LabelSelectorOpExists},
		},
	})

	NodeClaimPredicate = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			nodeclaim, ok := e.Object.(*karpenterv1.NodeClaim)
			if !ok {
				return false
			}
			return isRelevantNodeClaim(nodeclaim.GetLabels())
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNodeClaim, ok := e.ObjectOld.(*karpenterv1.NodeClaim)
			if !ok {
				return false
			}

			newNodeClaim, ok := e.ObjectNew.(*karpenterv1.NodeClaim)
			if !ok {
				return false
			}
			if !isRelevantNodeClaim(oldNodeClaim.GetLabels()) {
				return false
			}

			if !isRelevantNodeClaim(newNodeClaim.GetLabels()) {
				return false
			}

			oldNodeClaimCopy := oldNodeClaim.DeepCopy()
			newNodeClaimCopy := newNodeClaim.DeepCopy()

			// if only nodeclaim.Status.LastPodEventTime is changed, skip update event
			oldNodeClaimCopy.ResourceVersion = ""
			oldNodeClaimCopy.Status.LastPodEventTime = metav1.Time{}
			newNodeClaimCopy.ResourceVersion = ""
			newNodeClaimCopy.Status.LastPodEventTime = metav1.Time{}
			return !reflect.DeepEqual(oldNodeClaimCopy, newNodeClaimCopy)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			nodeclaim, ok := e.Object.(*karpenterv1.NodeClaim)
			if !ok {
				return false
			}
			return isRelevantNodeClaim(nodeclaim.GetLabels())
		},
	}
)

// isRelevantNodeClaim returns true if the NodeClaim has labels indicating it
// belongs to a Workspace (legacy kaito.sh/* or karpenter.kaito.sh/*) or a RAGEngine.
func isRelevantNodeClaim(lbls map[string]string) bool {
	s := labels.Set(lbls)
	return WorkspaceSelector.Matches(s) || KarpenterWorkspaceSelector.Matches(s) || RagEngineSelector.Matches(s)
}

type ManifestOptions struct {
	DefaultNodeImageFamily string
}

// GenerateNodeClaimManifest generates a nodeClaim object from the given workspace or RAGEngine.
func GenerateNodeClaimManifest(storageRequirement string, obj client.Object) *karpenterv1.NodeClaim {
	return GenerateNodeClaimManifestWithOptions(storageRequirement, obj, ManifestOptions{})
}

// GenerateNodeClaimManifestWithOptions generates a nodeClaim object from the given workspace or RAGEngine with explicit options.
func GenerateNodeClaimManifestWithOptions(storageRequirement string, obj client.Object, options ManifestOptions) *karpenterv1.NodeClaim {
	klog.InfoS("GenerateNodeClaimManifest", "object", obj)

	// Determine the type of the input object and extract relevant fields
	instanceType, namespace, name, labelSelector, nameLabel, namespaceLabel, err := nodes.ExtractObjFields(obj)
	if err != nil {
		klog.Error(err)
		return nil
	}

	nodeClaimName := GenerateNodeClaimName(obj)

	nodeClaimLabels := map[string]string{
		consts.LabelNodePool: consts.KaitoNodePoolName, // Fake nodepool name to prevent Karpenter from scaling up.
		nameLabel:            name,
		namespaceLabel:       namespace,
	}
	if sanitized := kaitov1beta1.SanitizedMatchLabels(labelSelector); len(sanitized) != 0 {
		nodeClaimLabels = lo.Assign(nodeClaimLabels, sanitized)
	}

	nodeClaimAnnotations := map[string]string{
		karpenterv1.DoNotDisruptAnnotationKey: "true", // To prevent Karpenter from scaling down.
	}

	if options.DefaultNodeImageFamily == "" {
		nodeClaimAnnotations[kaitov1beta1.AnnotationNodeImageFamily] = consts.NodeImageFamilyUbuntu
	} else if defaultNodeImageFamily, ok := consts.NormalizeSupportedNodeImageFamily(options.DefaultNodeImageFamily); ok {
		nodeClaimAnnotations[kaitov1beta1.AnnotationNodeImageFamily] = defaultNodeImageFamily
	}

	if nodeImageFamily, ok := obj.GetAnnotations()[kaitov1beta1.AnnotationNodeImageFamily]; ok {
		if normalizedNodeImageFamily, valid := consts.NormalizeSupportedNodeImageFamily(nodeImageFamily); valid {
			nodeClaimAnnotations[kaitov1beta1.AnnotationNodeImageFamily] = normalizedNodeImageFamily
		}
	}

	cloudName := os.Getenv("CLOUD_PROVIDER")

	var nodeClassRefKind string
	var nodeClassRefGroup string

	switch cloudName {
	case consts.AzureCloudName: //azure
		nodeClassRefKind = "KaitoNodeClass"
		nodeClassRefGroup = "kaito.sh"
	case consts.AWSCloudName: //aws
		nodeClassRefKind = "EC2NodeClass"
		nodeClassRefGroup = "karpenter.k8s.aws"
	}

	nodeClaimObj := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nodeClaimName,
			Namespace:   namespace,
			Labels:      nodeClaimLabels,
			Annotations: nodeClaimAnnotations,
		},
		Spec: karpenterv1.NodeClaimSpec{
			NodeClassRef: &karpenterv1.NodeClassReference{
				Name:  consts.NodeClassName,
				Kind:  nodeClassRefKind,
				Group: nodeClassRefGroup,
			},
			Taints: []corev1.Taint{
				{
					Key:    consts.SKUString,
					Value:  consts.GPUString,
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			Requirements: []karpenterv1.NodeSelectorRequirementWithMinValues{
				{
					Key:      consts.LabelNodePool,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{consts.KaitoNodePoolName},
				},
				{
					Key:      corev1.LabelInstanceTypeStable,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{instanceType},
				},
				{
					Key:      corev1.LabelOSStable,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"linux"},
				},
			},
			Resources: karpenterv1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageRequirement),
				},
			},
		},
	}

	if cloudName == consts.AzureCloudName {
		nodeSelector := karpenterv1.NodeSelectorRequirementWithMinValues{
			Key:      azurev1beta1.LabelSKUName,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{instanceType},
		}
		nodeClaimObj.Spec.Requirements = append(nodeClaimObj.Spec.Requirements, nodeSelector)
	}

	if cloudName == consts.AWSCloudName {
		nodeSelector := karpenterv1.NodeSelectorRequirementWithMinValues{
			Key:      "karpenter.k8s.aws/instance-gpu-count",
			Operator: corev1.NodeSelectorOpGt,
			Values:   []string{"0"},
		}
		nodeClaimObj.Spec.Requirements = append(nodeClaimObj.Spec.Requirements, nodeSelector)
	}

	return nodeClaimObj
}

// GenerateNodeClaimName generates a nodeClaim name from the given workspace or RAGEngine.
func GenerateNodeClaimName(obj client.Object) string {
	// Determine the type of the input object and extract relevant fields
	_, namespace, name, _, _, _, err := nodes.ExtractObjFields(obj)
	if err != nil {
		return ""
	}

	digest := sha256.Sum256([]byte(namespace + name + time.Now().Format("2006-01-02 15:04:05.000000000"))) // We make sure the nodeClaim name is not fixed to the object
	nodeClaimName := "ws" + hex.EncodeToString(digest[0:])[0:9]
	return nodeClaimName
}

// CreateNodeClaim creates a nodeClaim object.
func CreateNodeClaim(ctx context.Context, nodeClaimObj *karpenterv1.NodeClaim, kubeClient client.Client) error {
	klog.InfoS("CreateNodeClaim", "nodeClaim", klog.KObj(nodeClaimObj))
	return kubeClient.Create(ctx, nodeClaimObj, &client.CreateOptions{})
}

// WaitForPendingNodeClaims checks if there are any nodeClaims in provisioning condition. If so, wait until they are ready.
func WaitForPendingNodeClaims(ctx context.Context, obj client.Object, kubeClient client.Client) error {

	// Determine the type of the input object and retrieve the InstanceType
	instanceType, _, _, _, _, _, err := nodes.ExtractObjFields(obj)
	if err != nil {
		return err
	}

	nodeClaims, err := ListNodeClaim(ctx, obj, kubeClient)
	if err != nil {
		return err
	}

	for i := range nodeClaims.Items {
		// check if the nodeClaim being created has the requested instance type
		_, nodeClaimInstanceType := lo.Find(nodeClaims.Items[i].Spec.Requirements, func(requirement karpenterv1.NodeSelectorRequirementWithMinValues) bool {
			return requirement.Key == corev1.LabelInstanceTypeStable &&
				requirement.Operator == corev1.NodeSelectorOpIn &&
				lo.Contains(requirement.Values, instanceType)
		})
		if nodeClaimInstanceType {
			_, nodeClaimIsInitalized := lo.Find(nodeClaims.Items[i].GetConditions(), func(condition status.Condition) bool {
				return condition.Type == karpenterv1.ConditionTypeInitialized && condition.Status == metav1.ConditionTrue
			})

			if nodeClaimIsInitalized {
				continue
			}

			// wait until the nodeClaim is initialized
			if err := CheckNodeClaimStatus(ctx, &nodeClaims.Items[i], kubeClient); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListNodeClaim lists all nodeClaim objects in the cluster that are created by the given workspace or RAGEngine.
func ListNodeClaim(ctx context.Context, obj client.Object, kubeClient client.Client) (*karpenterv1.NodeClaimList, error) {
	nodeClaimList := &karpenterv1.NodeClaimList{}

	var ls labels.Set

	// Build label selector based on the type of the input object
	switch o := obj.(type) {
	case *kaitov1beta1.Workspace:
		ls = labels.Set{
			kaitov1beta1.LabelWorkspaceName:      o.Name,
			kaitov1beta1.LabelWorkspaceNamespace: o.Namespace,
		}
	case *kaitov1beta1.RAGEngine:
		ls = labels.Set{
			kaitov1beta1.LabelRAGEngineName:      o.Name,
			kaitov1beta1.LabelRAGEngineNamespace: o.Namespace,
		}
	default:
		return nil, fmt.Errorf("unsupported object type: %T", obj)
	}

	err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		return kubeClient.List(ctx, nodeClaimList, &client.MatchingLabelsSelector{Selector: ls.AsSelector()})
	})
	if err != nil {
		return nil, err
	}

	return nodeClaimList, nil
}

// CheckNodeClaimStatus checks the status of the nodeClaim. If the nodeClaim is not ready, then it will wait for the nodeClaim to be ready.
// If the nodeClaim is not ready after the timeout, then it will return an error.
// if the nodeClaim is ready, then it will return nil.
func CheckNodeClaimStatus(ctx context.Context, nodeClaimObj *karpenterv1.NodeClaim, kubeClient client.Client) error {
	klog.InfoS("CheckNodeClaimStatus", "nodeClaim", klog.KObj(nodeClaimObj))
	timeClock := clock.RealClock{}
	tick := timeClock.NewTicker(nodeClaimStatusTimeoutInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-tick.C():
			return fmt.Errorf("check nodeClaim status timed out. nodeClaim %s is not ready", nodeClaimObj.Name)

		default:
			time.Sleep(1 * time.Second)
			err := kubeClient.Get(ctx, client.ObjectKey{Name: nodeClaimObj.Name, Namespace: nodeClaimObj.Namespace}, nodeClaimObj, &client.GetOptions{})
			if err != nil {
				return err
			}

			// if SKU is not available, then no need to retry.
			_, conditionFound := lo.Find(nodeClaimObj.GetConditions(), func(condition status.Condition) bool {
				return condition.Type == karpenterv1.ConditionTypeLaunched &&
					condition.Status == metav1.ConditionFalse && strings.Contains(condition.Message, consts.ErrorInstanceTypesUnavailable)
			})
			if conditionFound {
				klog.Error(consts.ErrorInstanceTypesUnavailable, "reconcile will not continue")
				return reconcile.TerminalError(fmt.Errorf(consts.ErrorInstanceTypesUnavailable))
			}

			// if nodeClaim is not ready, then continue.
			_, conditionFound = lo.Find(nodeClaimObj.GetConditions(), func(condition status.Condition) bool {
				return condition.Type == string(apis.ConditionReady) &&
					condition.Status == metav1.ConditionTrue
			})
			if !conditionFound {
				continue
			}

			klog.InfoS("nodeClaim status is ready", "nodeClaim", nodeClaimObj.Name)
			return nil
		}
	}
}

// IsNodeClaimReadyNotDeleting checks if a NodeClaim is in ready state and not being deleted
func IsNodeClaimReadyNotDeleting(nodeClaim *karpenterv1.NodeClaim) bool {
	if !nodeClaim.DeletionTimestamp.IsZero() {
		return false
	}

	for _, condition := range nodeClaim.Status.Conditions {
		if condition.Type == "Ready" {
			return condition.Status == "True"
		}
	}

	return nodeClaim.Status.NodeName != ""
}

// maxProvisioningErrorMessageLen caps the surfaced message length for readability.
const maxProvisioningErrorMessageLen = 256

// awaitingReconciliationReason is the benign "not yet reconciled" state Karpenter
// core stamps on freshly-initialized conditions; it is not a provisioning error.
const awaitingReconciliationReason = consts.ReasonAwaitingReconciliation

// FirstNodeClaimProvisioningState returns the reason/message of the earliest
// non-True Karpenter lifecycle condition (Launched -> Registered -> Initialized)
// across the NodeClaims. A real provisioning error takes priority over the benign
// "AwaitingReconciliation" state. Deleting NodeClaims and empty messages are skipped.
func FirstNodeClaimProvisioningState(nodeClaims []*karpenterv1.NodeClaim) (reason, message string, found bool) {
	// A real error wins outright; otherwise fall back to the first in-progress state.
	var pending *status.Condition

	// Earlier lifecycle stages are the root cause, so scan them first.
	for _, stage := range []string{
		karpenterv1.ConditionTypeLaunched,
		karpenterv1.ConditionTypeRegistered,
		karpenterv1.ConditionTypeInitialized,
	} {
		for _, nc := range nodeClaims {
			if nc == nil || !nc.DeletionTimestamp.IsZero() {
				continue
			}
			for i := range nc.Status.Conditions {
				c := &nc.Status.Conditions[i]
				if c.Type != stage || c.Status == metav1.ConditionTrue || c.Message == "" {
					continue
				}
				if c.Reason == awaitingReconciliationReason {
					if pending == nil {
						pending = c
					}
					continue
				}
				return c.Reason, utils.TruncateMessage(c.Message, maxProvisioningErrorMessageLen), true
			}
		}
	}

	if pending != nil {
		return pending.Reason, utils.TruncateMessage(pending.Message, maxProvisioningErrorMessageLen), true
	}
	return "", "", false
}

var (
	// azureSDKErrorCodePattern matches the error code line of an Azure SDK error dump, e.g. "ERROR CODE: SubnetIsFull".
	azureSDKErrorCodePattern = regexp.MustCompile(`ERROR CODE: (\w+)`)
	// cloudErrorCodePatterns also match the codes of the JSON error body and of AKS machine provisioning errors.
	cloudErrorCodePatterns = []*regexp.Regexp{
		azureSDKErrorCodePattern,
		regexp.MustCompile(`"code":\s*"(\w+)"`),
		regexp.MustCompile(`\bcode=(\w+)`),
	}
	// cloudErrorMessagePattern matches the first JSON "message" field after the error code.
	cloudErrorMessagePattern = regexp.MustCompile(`"message":\s*"((?:[^"\\]|\\.)*)"`)
)

// CloudErrorCodes returns the cloud error codes in a provisioning error message.
func CloudErrorCodes(msg string) sets.Set[string] {
	codes := sets.New[string]()
	for _, pattern := range cloudErrorCodePatterns {
		for _, match := range pattern.FindAllStringSubmatch(msg, -1) {
			codes.Insert(match[1])
		}
	}
	return codes
}

// compactProvisioningMessage moves the error code and message of a verbose cloud error dump to the front, so truncation keeps the cause.
func compactProvisioningMessage(msg string) string {
	code := azureSDKErrorCodePattern.FindStringSubmatch(msg)
	if code == nil {
		return msg
	}
	compact := code[1]
	if m := cloudErrorMessagePattern.FindStringSubmatch(msg[strings.Index(msg, code[0]):]); m != nil {
		text := m[1]
		if unquoted, err := strconv.Unquote(`"` + text + `"`); err == nil {
			text = unquoted
		}
		compact += ": " + text
	}
	if first, _, _ := strings.Cut(msg, "\n"); strings.TrimSpace(first) != "" {
		compact += " (" + strings.TrimSpace(first) + ")"
	}
	return compact
}

// FormatProvisioningMessage compacts and truncates a provisioning error message for a condition.
func FormatProvisioningMessage(msg string) string {
	return utils.TruncateMessage(compactProvisioningMessage(msg), maxProvisioningErrorMessageLen)
}

// FormatClassifiedProvisioningMessage formats the message of a failure reported under another reason, keeping the
// original reason in front.
func FormatClassifiedProvisioningMessage(originalReason, msg string) string {
	return utils.TruncateMessage(originalReason+": "+compactProvisioningMessage(msg), maxProvisioningErrorMessageLen)
}

// ParseClassifiedProvisioningMessage splits a message formatted by FormatClassifiedProvisioningMessage into the
// original reason and the formatted original message. originalReason is empty if msg has no reason in front.
func ParseClassifiedProvisioningMessage(msg string) (originalReason, originalMessage string) {
	originalReason, originalMessage, found := strings.Cut(msg, ": ")
	if !found {
		return "", msg
	}
	return originalReason, originalMessage
}

// nodeClassReadinessUnknownReason is the Launched reason recorded while the NodeClass is not reconciled yet; it is not an error.
const nodeClassReadinessUnknownReason = "NodeClassReadinessUnknown"

// NodeClaimNotReadyReason is reported while NodeClaims are provisioning without an observed failure.
const NodeClaimNotReadyReason = consts.ReasonNodeClaimNotReady

// lifecycleStages are the NodeClaim lifecycle conditions in order; an earlier stage causes a later one.
var lifecycleStages = []string{
	karpenterv1.ConditionTypeLaunched,
	karpenterv1.ConditionTypeRegistered,
	karpenterv1.ConditionTypeInitialized,
}

// isLifecycleFailure reports a failed latest attempt: a False condition, or Launched=Unknown with a cloud error reason.
func isLifecycleFailure(c *status.Condition) bool {
	if c.Status == metav1.ConditionFalse {
		return true
	}
	return c.Type == karpenterv1.ConditionTypeLaunched && c.Status == metav1.ConditionUnknown && c.Reason != "" &&
		c.Reason != awaitingReconciliationReason && c.Reason != nodeClassReadinessUnknownReason
}

func describeCondition(c *status.Condition) string {
	text := fmt.Sprintf("NodeClaim %s=%s", c.Type, c.Status)
	if c.Reason != "" {
		text += fmt.Sprintf(" (%s)", c.Reason)
	}
	if c.Message != "" {
		text += ": " + c.Message
	}
	return text
}

func lifecycleCondition(nc *karpenterv1.NodeClaim, conditionType string) *status.Condition {
	for i := range nc.Status.Conditions {
		if nc.Status.Conditions[i].Type == conditionType {
			return &nc.Status.Conditions[i]
		}
	}
	return nil
}

// failureMessage describes a failed condition that has no message.
func failureMessage(c *status.Condition) string {
	if c.Message == "" {
		return describeCondition(c)
	}
	return c.Message
}

// IsConditionTrue reports whether the NodeClaim's condition of the given type is True.
func IsConditionTrue(nc *karpenterv1.NodeClaim, conditionType string) bool {
	c := lifecycleCondition(nc, conditionType)
	return c != nil && c.Status == metav1.ConditionTrue
}

// RawNodeClaimProvisioningFailure returns the earliest-stage failure across the NodeClaims with its message as
// recorded, preferring live NodeClaims to deleting ones, which keep the failure of an attempt Karpenter gave up on.
func RawNodeClaimProvisioningFailure(nodeClaims []*karpenterv1.NodeClaim) (reason, message string, found bool) {
	if reason, message, found = provisioningFailure(nodeClaims, false); found {
		return reason, message, found
	}
	return provisioningFailure(nodeClaims, true)
}

func provisioningFailure(nodeClaims []*karpenterv1.NodeClaim, deleting bool) (reason, message string, found bool) {
	for _, stage := range lifecycleStages {
		for _, nc := range nodeClaims {
			if nc == nil || nc.DeletionTimestamp.IsZero() == deleting {
				continue
			}
			if c := lifecycleCondition(nc, stage); c != nil && isLifecycleFailure(c) {
				return c.Reason, failureMessage(c), true
			}
		}
	}
	return "", "", false
}

// NodeClaimProvisioningProgress describes the earliest-stage lifecycle condition not yet True across the live NodeClaims.
func NodeClaimProvisioningProgress(nodeClaims []*karpenterv1.NodeClaim) (message string, found bool) {
	for _, stage := range lifecycleStages {
		for _, nc := range nodeClaims {
			if nc == nil || !nc.DeletionTimestamp.IsZero() {
				continue
			}
			if c := lifecycleCondition(nc, stage); c != nil && c.Status != metav1.ConditionTrue {
				return FormatProvisioningMessage(describeCondition(c)), true
			}
		}
	}
	return "", false
}

// ProvisioningFailedReason replaces a failure reason that is not a valid condition reason.
const ProvisioningFailedReason = "NodeProvisioningFailed"

// conditionReasonPattern is the metav1.Condition reason format enforced by CRD validation.
var conditionReasonPattern = regexp.MustCompile(`^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`)

// ValidConditionReason returns reason if it is a valid condition reason, and ProvisioningFailedReason otherwise.
func ValidConditionReason(reason string) string {
	if len(reason) > 1024 || !conditionReasonPattern.MatchString(reason) {
		return ProvisioningFailedReason
	}
	return reason
}
