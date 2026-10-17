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
	"testing"
	"time"

	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
)

const (
	testPoolName = "default-ws1"
	testPoolUID  = types.UID("pool-uid-1")
)

// newWorkspaceNodeClaim builds a KAITO-managed NodeClaim owned by the given NodePool.
func newWorkspaceNodeClaim(name string, poolUID types.UID, conds ...status.Condition) *karpenterv1.NodeClaim {
	return &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(name + "-uid"),
			Labels: map[string]string{
				karpenterv1.NodePoolLabelKey:          testPoolName,
				consts.KarpenterWorkspaceNameKey:      "ws1",
				consts.KarpenterWorkspaceNamespaceKey: "default",
			},
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "karpenter.sh/v1", Kind: "NodePool", Name: testPoolName, UID: poolUID},
			},
		},
		Status: karpenterv1.NodeClaimStatus{Conditions: conds},
	}
}

func awaitingLaunch() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown,
		Reason: "AwaitingReconciliation", Message: "object is awaiting reconciliation"}
}

// quotaLaunchFailure is what the Azure provider records when a VM create hits the vCPU quota.
func quotaLaunchFailure() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown,
		Reason: "CreateInstanceFailed", Message: "creating instance failed, subscription level on-demand vCPU quota for Standard_NC24ads_A100_v4 has been reached"}
}

func throttledLaunchFailure() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown,
		Reason: "CreateInstanceFailed", Message: "creating instance failed, TooManyRequests"}
}

func launchedOK() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionTrue, Reason: "Launched"}
}

func registeredOK() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionTrue, Reason: "Registered"}
}

func awaitingRegistration() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionUnknown,
		Reason: "NodeNotFound", Message: "Node not registered with cluster"}
}

func newTestTracker(now *time.Time) *provisioningFailureTracker {
	t := newProvisioningFailureTracker()
	t.now = func() time.Time { return *now }
	t.track(testPoolName, testPoolUID)
	return t
}

func assertRootCause(t *testing.T, tr *provisioningFailureTracker, tier failureTier, reason string) {
	t.Helper()
	c, ok := tr.rootCause(testPoolName)
	if assert.True(t, ok, "expected a root cause") {
		assert.Equal(t, tier, c.tier)
		assert.Equal(t, reason, c.reason)
	}
}

func TestProvisioningFailureTracker_IgnoresUntrackedNodePools(t *testing.T) {
	now := time.Now()
	tr := newTestTracker(&now)

	other := newWorkspaceNodeClaim("nc-other", "other-pool-uid", quotaLaunchFailure())
	tr.observe(nil, other, true)
	_, ok := tr.rootCause(testPoolName)
	assert.False(t, ok, "a NodeClaim of another NodePool with the same name is ignored")

	unlabeled := newWorkspaceNodeClaim("nc-unlabeled", testPoolUID, quotaLaunchFailure())
	delete(unlabeled.Labels, karpenterv1.NodePoolLabelKey)
	tr.observe(nil, unlabeled, true)
	_, ok = tr.rootCause(testPoolName)
	assert.False(t, ok, "a NodeClaim without a NodePool label is ignored")

	tr.untrack(testPoolName)
	tr.observe(nil, newWorkspaceNodeClaim("nc-gc", testPoolUID, quotaLaunchFailure()), true)
	assert.Empty(t, tr.pools, "NodeClaims garbage-collected after DeleteNodes leave nothing behind")
}

func TestProvisioningFailureTracker_TrackReplacesRecreatedNodePool(t *testing.T) {
	now := time.Now()
	tr := newTestTracker(&now)
	tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, quotaLaunchFailure()), false)

	tr.track(testPoolName, testPoolUID)
	assertRootCause(t, tr, tierCloudError, "CreateInstanceFailed")

	tr.track(testPoolName, "pool-uid-2")
	_, ok := tr.rootCause(testPoolName)
	assert.False(t, ok, "a recreated NodePool does not inherit its predecessor's failures")
}

func TestProvisioningFailureTracker_KeepsCloudErrorAfterDeletion(t *testing.T) {
	now := time.Now()
	tr := newTestTracker(&now)
	failed := newWorkspaceNodeClaim("nc-1", testPoolUID, quotaLaunchFailure())
	tr.observe(nil, failed, false)
	tr.observe(nil, failed, true)

	c, ok := tr.rootCause(testPoolName)
	assert.True(t, ok)
	assert.Equal(t, rootCause{tier: tierCloudError, reason: "CreateInstanceFailed", message: quotaLaunchFailure().Message}, c)
	assert.Empty(t, tr.eventLookupCandidates(testPoolName), "no Event lookup once the cloud error is known")
}

func TestProvisioningFailureTracker_RecordsDeletionStage(t *testing.T) {
	t.Run("deleted before launch", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(&now)
		tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, awaitingLaunch()), true)
		assertRootCause(t, tr, tierDeletedOnly, NodeClaimLaunchFailedReason)
	})
	t.Run("deleted after launch before registration", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(&now)
		tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, launchedOK(), awaitingRegistration()), true)
		assertRootCause(t, tr, tierDeletedOnly, NodeClaimRegistrationFailedReason)
	})
	t.Run("deleted after registration is normal lifecycle", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(&now)
		tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, launchedOK(), registeredOK()), true)
		_, ok := tr.rootCause(testPoolName)
		assert.False(t, ok)
	})
}

func TestProvisioningFailureTracker_RootCauseRanking(t *testing.T) {
	t.Run("same tier keeps the first failure", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(&now)
		tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, quotaLaunchFailure()), true)
		tr.observe(nil, newWorkspaceNodeClaim("nc-2", testPoolUID, throttledLaunchFailure()), false)
		c, _ := tr.rootCause(testPoolName)
		assert.Equal(t, quotaLaunchFailure().Message, c.message)
	})
	t.Run("a deletion stage does not replace a cloud error", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(&now)
		tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, quotaLaunchFailure()), false)
		tr.observe(nil, newWorkspaceNodeClaim("nc-2", testPoolUID, awaitingLaunch()), true)
		assertRootCause(t, tr, tierCloudError, "CreateInstanceFailed")
	})
	t.Run("a cloud error replaces an insufficient capacity Event", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(&now)
		tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, awaitingLaunch()), true)
		d := tr.eventLookupCandidates(testPoolName)[0]
		tr.recordEventLookup(testPoolName, d, insufficientCapacityEventReason, "all requested instance types were unavailable during launch", true)
		assertRootCause(t, tr, tierInsufficientCapacity, insufficientCapacityEventReason)

		tr.observe(nil, newWorkspaceNodeClaim("nc-2", testPoolUID, quotaLaunchFailure()), false)
		assertRootCause(t, tr, tierCloudError, "CreateInstanceFailed")
	})
	t.Run("an asynchronous provisioning error replaces an insufficient capacity Event", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(&now)
		tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, launchedOK()), true)
		tr.observe(nil, newWorkspaceNodeClaim("nc-2", testPoolUID, awaitingLaunch()), true)
		candidates := tr.eventLookupCandidates(testPoolName)
		assert.Len(t, candidates, 2)
		tr.recordEventLookup(testPoolName, candidates[1], insufficientCapacityEventReason, "no instance types available", true)
		tr.recordEventLookup(testPoolName, candidates[0], asyncProvisioningEventReason, "Failed to register: AllocationFailed", true)
		assertRootCause(t, tr, tierCloudError, asyncProvisioningEventReason)
	})
	t.Run("an invalid reason is replaced", func(t *testing.T) {
		now := time.Now()
		tr := newTestTracker(&now)
		tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, status.Condition{
			Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "bad reason!", Message: "boom"}), false)
		assertRootCause(t, tr, tierCloudError, nodeclaim.ProvisioningFailedReason)
	})
}

func TestProvisioningFailureTracker_RegistrationClearsFailures(t *testing.T) {
	now := time.Now()
	tr := newTestTracker(&now)
	tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, awaitingLaunch()), true)
	booting := newWorkspaceNodeClaim("nc-2", testPoolUID, launchedOK(), awaitingRegistration())
	registered := newWorkspaceNodeClaim("nc-2", testPoolUID, launchedOK(), registeredOK())

	tr.observe(nil, booting, false)
	assertRootCause(t, tr, tierDeletedOnly, NodeClaimLaunchFailedReason)

	tr.observe(booting, registered, false)
	_, ok := tr.rootCause(testPoolName)
	assert.False(t, ok, "the registration of a NodeClaim marks the NodePool recovered")
	assert.Empty(t, tr.eventLookupCandidates(testPoolName))

	tr.observe(nil, newWorkspaceNodeClaim("nc-3", testPoolUID, quotaLaunchFailure()), false)
	tr.observe(registered, registered, false)
	assertRootCause(t, tr, tierCloudError, "CreateInstanceFailed")
}

func TestProvisioningFailureTracker_EventLookupCandidates(t *testing.T) {
	now := time.Now()
	tr := newTestTracker(&now)
	for _, name := range []string{"nc-1", "nc-2", "nc-3", "nc-4"} {
		tr.observe(nil, newWorkspaceNodeClaim(name, testPoolUID, awaitingLaunch()), true)
		tr.observe(nil, newWorkspaceNodeClaim(name, testPoolUID, awaitingLaunch()), true)
	}
	assert.Equal(t, []string{"nc-2", "nc-3", "nc-4"}, candidateNames(tr.eventLookupCandidates(testPoolName)),
		"at most maxPendingEventLookups NodeClaims, the oldest dropped, each once")

	now = now.Add(eventLookupInterval - time.Second)
	assert.Empty(t, tr.eventLookupCandidates(testPoolName), "lookups are throttled per NodePool")

	now = now.Add(time.Second)
	candidates := tr.eventLookupCandidates(testPoolName)
	assert.Len(t, candidates, 3)
	tr.recordEventLookup(testPoolName, candidates[0], "", "", false)
	tr.recordEventLookup(testPoolName, candidates[1], insufficientCapacityEventReason, "no instance types available", true)

	now = now.Add(eventLookupInterval)
	assert.Equal(t, []string{"nc-2", "nc-4"}, candidateNames(tr.eventLookupCandidates(testPoolName)),
		"a NodeClaim without a failure Event is looked up again, one with a failure Event is not")

	now = now.Add(pendingEventLookupTTL)
	assert.Empty(t, tr.eventLookupCandidates(testPoolName), "NodeClaims deleted long ago are not looked up any more")
}

func TestProvisioningFailureTracker_DropsEventLookupResultsAfterRegistration(t *testing.T) {
	now := time.Now()
	tr := newTestTracker(&now)
	tr.observe(nil, newWorkspaceNodeClaim("nc-1", testPoolUID, awaitingLaunch()), true)
	d := tr.eventLookupCandidates(testPoolName)[0]

	tr.observe(nil, newWorkspaceNodeClaim("nc-2", testPoolUID, launchedOK(), registeredOK()), false)
	tr.recordEventLookup(testPoolName, d, insufficientCapacityEventReason, "no instance types available", true)
	_, ok := tr.rootCause(testPoolName)
	assert.False(t, ok)
}

func candidateNames(candidates []deletedNodeClaim) []string {
	names := make([]string, 0, len(candidates))
	for _, d := range candidates {
		names = append(names, d.name)
	}
	return names
}
