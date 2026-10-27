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

package karpenter

import (
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
)

const (
	// registrationFailedReason is the NodePool NodeRegistrationHealthy=False reason written by Karpenter when launched
	// nodes repeatedly fail to register.
	registrationFailedReason = "RegistrationFailed"
	// subscriptionQuotaReachedReason is the Launched reason of the Azure provider for an exhausted vCPU quota.
	subscriptionQuotaReachedReason = "SubscriptionQuotaReached"
)

var (
	// noOfferingMessages are the insufficient capacity errors of a NodeClaim whose instance types have no available
	// offering, which do not say why. KAITO NodePools allow a single instance type and capacity type, so its only
	// offering is either cached as unavailable after an earlier quota, allocation or SKU failure, whose NodeClaim is
	// deleted, or not offered at all.
	noOfferingMessages = []string{"all requested instance types were unavailable", "no instance types available"}
	// nodeClassResolutionMessages are the insufficient capacity errors the Azure and AWS providers report when they
	// cannot resolve the NodeClaim's NodeClass. The NodeClass is read directly on every reconcile and reported as
	// NodeClassInvalid while it is missing, so these errors are either redundant or stale once it is restored.
	nodeClassResolutionMessages = []string{"resolving node class", "resolving nodeclass"}
	// nodePoolOrNodeClassReasons report an invalid NodePool or a failed NodeClass, read directly on every reconcile.
	nodePoolOrNodeClassReasons = sets.New(consts.ReasonNodeClassInvalid, consts.ReasonNodePoolInvalid)
)

// failurePattern identifies a kind of provisioning failure by its reason, its cloud error codes or its message.
type failurePattern struct {
	reasons sets.Set[string]
	codes   sets.Set[string]
	// qualifiedCodes are generic cloud error codes that identify the failure only with a message matching the pattern.
	qualifiedCodes map[string]*regexp.Regexp
	// messages are lowercase substrings of the message.
	messages []string
}

func (p failurePattern) matches(reason, message, lowerMessage string, codes sets.Set[string]) bool {
	if p.reasons.Has(reason) || codes.HasAny(p.codes.UnsortedList()...) || containsAny(lowerMessage, p.messages...) {
		return true
	}
	for code, pattern := range p.qualifiedCodes {
		if codes.Has(code) && pattern.MatchString(message) {
			return true
		}
	}
	return false
}

// provisioningFailurePatterns are the KAITO reasons of the provisioning failures that need the user to act, in the
// order they are matched. They match the Launched reasons of recent Azure providers, the messages of their error
// handlers, which older providers report as CreateInstanceFailed or in Events, and the cloud error codes.
var provisioningFailurePatterns = []struct {
	reason  string
	pattern failurePattern
}{
	{reason: consts.ReasonQuotaExceeded, pattern: failurePattern{
		reasons:        sets.New(subscriptionQuotaReachedReason),
		codes:          sets.New("QuotaExceeded"),
		qualifiedCodes: map[string]*regexp.Regexp{"OperationNotAllowed": regexp.MustCompile(`(?i)quota`)},
		messages:       []string{"vcpu quota", "lowpriorityquota"},
	}},
	{reason: consts.ReasonCapacityUnavailable, pattern: failurePattern{
		reasons: sets.New("AllocationFailure", "OverconstrainedAllocationFailure", "SKUNotAvailable"),
		codes: sets.New("AllocationFailed", "ZonalAllocationFailed", "OverconstrainedAllocationRequest",
			"OverconstrainedZonalAllocationRequest", "SkuNotAvailable", "VMSizeNotSupported"),
		// Written by the Azure provider's allocation and SKU error handlers.
		messages: []string{"unable to allocate resources", "the requested sku is unavailable",
			"is not supported for this subscription"},
	}},
	{reason: consts.ReasonNodeImageInvalid, pattern: failurePattern{
		codes: sets.New("ImageNotFound", "GalleryImageNotFound"),
		// An invalid parameter is the image reference when its message ("The value of parameter imageReference is
		// invalid") or target names it, rather than when the message only mentions it.
		qualifiedCodes: map[string]*regexp.Regexp{
			"InvalidParameter": regexp.MustCompile(`(?i)parameter\s+'?imageReference\b|"target":\s*"imageReference\b`),
		},
		// Written by the Azure provider when the NodeClass has no image for the instance type.
		messages: []string{"no compatible images found for instance type"},
	}},
	{reason: consts.ReasonNodeBootstrapFailed, pattern: failurePattern{
		reasons: sets.New(registrationFailedReason),
		codes:   sets.New("VMExtensionProvisioningError"),
	}},
	{reason: consts.ReasonNodeConfigurationInvalid, pattern: failurePattern{
		// Recur on every retry until the user fixes a permission, a subnet, an Azure Policy, a resource provider
		// registration or a referenced resource.
		codes: sets.New("AuthorizationFailed", "LinkedAuthorizationFailed", "SubnetIsFull", "InsufficientSubnetSize",
			"RequestDisallowedByPolicy", "MissingSubscriptionRegistration", "InvalidResourceReference"),
	}},
}

func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// zonalCapacityFailure is a synchronous capacity failure in the selected zone. The provider caches only that zone as
// unavailable and its next attempt, about a second later, tries another zone, so it is not CapacityUnavailable until
// every zone failed, which the provider reports as a no-offering insufficient capacity error. An asynchronous one is
// still CapacityUnavailable: its Event outranks the no-offering Events that follow it for nodeClaimEventWindow.
var zonalCapacityFailure = failurePattern{
	reasons: sets.New("ZonalAllocationFailure", "OverconstrainedZonalAllocationFailure"),
	// Written by the Azure provider's zonal allocation error handlers, which older providers report as
	// CreateInstanceFailed.
	messages: []string{"in the selected zone"},
}

// classifyProvisioningFailure returns the KAITO reason of a provisioning failure that needs the user to act, or ""
// otherwise: the first of provisioningFailurePatterns it matches, unless it is a synchronous capacity failure in a
// single zone. Any other insufficient capacity error is CapacityUnavailable, as KAITO NodePools have a single offering
// to fall back on, unless the provider could not resolve the NodeClass.
func classifyProvisioningFailure(reason, message string) string {
	codes := nodeclaim.CloudErrorCodes(message)
	lower := strings.ToLower(message)
	if reason != asyncProvisioningEventReason && zonalCapacityFailure.matches(reason, message, lower, codes) {
		return ""
	}
	for _, p := range provisioningFailurePatterns {
		if p.pattern.matches(reason, message, lower, codes) {
			return p.reason
		}
	}
	if reason == insufficientCapacityEventReason && !isNodeClassResolutionFailure(reason, message) {
		return consts.ReasonCapacityUnavailable
	}
	return ""
}

// isNodeClassResolutionFailure reports an insufficient capacity error of a provider that could not resolve the
// NodeClass.
func isNodeClassResolutionFailure(reason, message string) bool {
	return reason == insufficientCapacityEventReason && containsAny(strings.ToLower(message), nodeClassResolutionMessages...)
}

// provisioningCause returns the reason and message reported for a provisioning failure: a failure that needs the user
// to act is reported under its KAITO reason, with the original reason in front of the message.
func provisioningCause(reason, message string) (string, string) {
	if classified := classifyProvisioningFailure(reason, message); classified != "" {
		return classified, nodeclaim.FormatClassifiedProvisioningMessage(reason, message)
	}
	return nodeclaim.ValidConditionReason(reason), nodeclaim.FormatProvisioningMessage(message)
}

// isNoOfferingAvailable reports an insufficient capacity error that no instance type offering is available, which
// does not say why.
func isNoOfferingAvailable(reason, message string) bool {
	return reason == insufficientCapacityEventReason && containsAny(strings.ToLower(message), noOfferingMessages...)
}

// reportedNotReadyCause is the NodeClaimReady failure already reported on the Workspace.
type reportedNotReadyCause struct {
	reason  string
	message string
	// noOffering reports a failure classified from a no-offering error.
	noOffering bool
}

// reportedCause returns the NodeClaimReady failure already reported on the Workspace while it still applies. The
// NodeClaim that carried a failure is deleted, and its retries often fail before launching with an error that does not
// say why, so the failure is kept on the Workspace until a new attempt launches.
func reportedCause(ws *kaitov1beta1.Workspace, nodeClaims []*karpenterv1.NodeClaim, now time.Time) (reportedNotReadyCause, bool) {
	c := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeNodeClaimStatus))
	if c == nil || c.Status != metav1.ConditionFalse || c.ObservedGeneration != ws.Generation {
		return reportedNotReadyCause{}, false
	}
	// Only a failure that needs the user to act is kept: progress, or a failure KAITO does not classify, says
	// nothing once its NodeClaim is gone.
	if !consts.IsTerminatedReason(c.Reason) {
		return reportedNotReadyCause{}, false
	}
	// The NodePool and NodeClass are read directly on every reconcile, so a failure of theirs is never kept once fixed.
	if nodePoolOrNodeClassReasons.Has(c.Reason) {
		return reportedNotReadyCause{}, false
	}
	message, _, _ := strings.Cut(c.Message, nodeprovision.ReconcileErrorMessagePrefix)
	originalReason, originalMessage := nodeclaim.ParseClassifiedProvisioningMessage(message)
	// The failure of the earlier attempts no longer applies once a new attempt launched.
	if hasUnregisteredLaunch(nodeClaims, now, 0) {
		return reportedNotReadyCause{}, false
	}
	return reportedNotReadyCause{
		reason:     c.Reason,
		message:    message,
		noOffering: isNoOfferingAvailable(originalReason, originalMessage),
	}, true
}
