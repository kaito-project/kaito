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
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
)

const (
	testPoolName = "default-ws1"
	testPoolUID  = types.UID("pool-uid-1")
	// testNodeClaimName is generated from testPoolName like the API server does for Karpenter's generateName.
	testNodeClaimName = testPoolName + "-bcdfg"
)

var testNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// azureAllocationFailedError mimics the Azure provider's error for a VM create that failed asynchronously.
const azureAllocationFailedError = `Failed to register: creating instance failed, PUT https://management.azure.com/subscriptions/0000/resourceGroups/MC_rg/providers/Microsoft.Compute/virtualMachines/aks-default-ws1-abcde
--------------------------------------------------------------------------------
RESPONSE 200: 200 OK
ERROR CODE: AllocationFailed
--------------------------------------------------------------------------------
{
  "error": {
    "code": "AllocationFailed",
    "message": "Allocation failed. We do not have sufficient capacity for the requested VM size in this region."
  }
}`

const (
	regionalQuotaMessage = "NodeClaim " + testNodeClaimName + " event: insufficient capacity, regional on-demand vCPU quota limit for subscription has been reached. " +
		"To scale beyond this limit, please review the quota increase process here: https://learn.microsoft.com/en-us/azure/quotas/regional-quota-requests"
	noOfferingMessage                    = "NodeClaim " + testNodeClaimName + " event: insufficient capacity, all requested instance types were unavailable during launch"
	noOfferingCapacityUnavailableMessage = insufficientCapacityEventReason + ": " + noOfferingMessage
	quotaLaunchMessage                   = "creating instance failed, subscription level on-demand vCPU quota for Standard_NC24ads_A100_v4 has been reached (may try provision an alternative instance type)"
)

// newWorkspaceNodeClaim builds a KAITO-managed NodeClaim of the test NodePool.
func newWorkspaceNodeClaim(name string, conds ...status.Condition) *karpenterv1.NodeClaim {
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
				{APIVersion: "karpenter.sh/v1", Kind: "NodePool", Name: testPoolName, UID: testPoolUID},
			},
		},
		Status: karpenterv1.NodeClaimStatus{Conditions: conds},
	}
}

func deletingNodeClaim(nc *karpenterv1.NodeClaim) *karpenterv1.NodeClaim {
	nc.DeletionTimestamp = &metav1.Time{Time: testNow}
	nc.Finalizers = []string{karpenterv1.TerminationFinalizer}
	return nc
}

func awaitingLaunch() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown,
		Reason: "AwaitingReconciliation", Message: "object is awaiting reconciliation"}
}

// quotaLaunchFailure is what Azure providers before v1.14 record when a VM create hits the vCPU quota.
func quotaLaunchFailure() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown,
		Reason: "CreateInstanceFailed", Message: quotaLaunchMessage}
}

func launchedOK() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionTrue, Reason: "Launched"}
}

// launchedAt is a Launched condition that turned True the given duration before testNow.
func launchedAt(ago time.Duration) status.Condition {
	c := launchedOK()
	c.LastTransitionTime = metav1.NewTime(testNow.Add(-ago))
	return c
}

func registeredOK() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionTrue, Reason: "Registered"}
}

func initializedOK() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeInitialized, Status: metav1.ConditionTrue, Reason: "Initialized"}
}

func readyOK() status.Condition {
	return status.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}
}

func awaitingRegistration() status.Condition {
	return status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionUnknown,
		Reason: "NodeNotFound", Message: "Node not registered with cluster"}
}

func testNodePool(conds ...status.Condition) *karpenterv1.NodePool {
	return &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: testPoolName, UID: testPoolUID, CreationTimestamp: metav1.NewTime(testNow.Add(-time.Hour))},
		Status:     karpenterv1.NodePoolStatus{Conditions: conds},
	}
}

func failureEvent(name, nodeClaimName, eventType, reason, message string, at time.Time) *corev1.Event {
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: nodeClaimEventNamespace},
		InvolvedObject: corev1.ObjectReference{
			APIVersion: "karpenter.sh/v1", Kind: "NodeClaim", Name: nodeClaimName, UID: types.UID(nodeClaimName + "-uid"),
		},
		Type: eventType, Reason: reason, Message: message,
		LastTimestamp: metav1.NewTime(at),
	}
}

// eventListCounter counts Event list calls, records their field selectors and can fail them.
type eventListCounter struct {
	calls     int
	selectors []string
	err       error
}

// newFailureTestClient builds a fake client supporting the NodeClaim Event field selectors.
func newFailureTestClient(counter *eventListCounter, objs ...client.Object) client.Client {
	b := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(objs...).
		WithIndex(&corev1.Event{}, eventFieldInvolvedObjectKind, func(o client.Object) []string {
			return []string{o.(*corev1.Event).InvolvedObject.Kind}
		}).
		WithIndex(&corev1.Event{}, eventFieldType, func(o client.Object) []string {
			return []string{o.(*corev1.Event).Type}
		})
	if counter != nil {
		b = b.WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.EventList); ok {
					counter.calls++
					listOpts := &client.ListOptions{}
					listOpts.ApplyOptions(opts)
					if listOpts.Namespace != nodeClaimEventNamespace {
						return errors.New("NodeClaim Events must be listed in the default namespace")
					}
					if listOpts.FieldSelector != nil {
						counter.selectors = append(counter.selectors, listOpts.FieldSelector.String())
					}
					if counter.err != nil {
						return counter.err
					}
				}
				return cl.List(ctx, list, opts...)
			},
		})
	}
	return b.Build()
}

// baseObjects returns a ready NodeClass, the test NodePool and the given objects.
func baseObjects(objs ...client.Object) []client.Object {
	return append([]client.Object{makeNodeClassUnstructured(testConfig.DefaultName), testNodePool()}, objs...)
}

func newFailureTestProvisioner(c client.Client) *KarpenterProvisioner {
	p := NewKarpenterProvisioner(c, testConfig)
	p.now = func() time.Time { return testNow }
	return p
}

func testWorkspace(targetNodeCount int32) *kaitov1beta1.Workspace {
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", targetNodeCount, nil, nil)
	ws.Generation = 1
	return ws
}

func nodeClaimConditions(t *testing.T, p *KarpenterProvisioner, ws *kaitov1beta1.Workspace) (nodeClaimCond, resourceCond metav1.Condition) {
	t.Helper()
	require.Equal(t, testPoolName, NodePoolName(ws.Namespace, ws.Name))
	conditions, err := p.CollectNodeStatusInfo(context.Background(), ws)
	require.NoError(t, err)
	byType := map[string]metav1.Condition{}
	for _, c := range conditions {
		byType[c.Type] = c
	}
	return byType[string(kaitov1beta1.ConditionTypeNodeClaimStatus)], byType[string(kaitov1beta1.ConditionTypeResourceStatus)]
}

func nodeClaimCondition(t *testing.T, p *KarpenterProvisioner, targetNodeCount int32) metav1.Condition {
	t.Helper()
	nodeClaimCond, _ := nodeClaimConditions(t, p, testWorkspace(targetNodeCount))
	return nodeClaimCond
}

// A quota error recorded by an Azure provider before v1.14 is reported as QuotaExceeded without listing Events.
func TestProvisioningFailure_NodeClaimFailureIsClassified(t *testing.T) {
	failing := newWorkspaceNodeClaim(testNodeClaimName, quotaLaunchFailure())
	counter := &eventListCounter{}
	p := newFailureTestProvisioner(newFailureTestClient(counter, baseObjects(failing)...))

	nodeClaimCond, resourceCond := nodeClaimConditions(t, p, testWorkspace(1))
	assert.Equal(t, metav1.ConditionFalse, nodeClaimCond.Status)
	assert.Equal(t, consts.ReasonQuotaExceeded, nodeClaimCond.Reason)
	assert.Equal(t, "CreateInstanceFailed: "+quotaLaunchMessage, nodeClaimCond.Message)
	assert.Equal(t, nodeClaimCond.Reason, resourceCond.Reason)
	assert.Equal(t, nodeClaimCond.Message, resourceCond.Message)
	assert.Zero(t, counter.calls, "Events are not listed when a NodeClaim carries the failure")
}

// A failure Karpenter does not classify keeps its own reason.
func TestProvisioningFailure_UnclassifiedNodeClaimFailure(t *testing.T) {
	failing := newWorkspaceNodeClaim(testNodeClaimName, launchedOK(), status.Condition{
		Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionFalse, Reason: "MultipleNodesFound",
		Message: "Invariant violated, matched multiple nodes"})
	p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(failing)...))

	nodeClaimCond := nodeClaimCondition(t, p, 1)
	assert.Equal(t, "MultipleNodesFound", nodeClaimCond.Reason)
	assert.Equal(t, "Invariant violated, matched multiple nodes", nodeClaimCond.Message)
}

// A deleting NodeClaim keeps the failure of the attempt Karpenter gave up on, but a live failure is preferred.
func TestProvisioningFailure_DeletingNodeClaimFailure(t *testing.T) {
	deleting := deletingNodeClaim(newWorkspaceNodeClaim(testPoolName+"-hjklm", quotaLaunchFailure()))
	replacement := newWorkspaceNodeClaim(testNodeClaimName, awaitingLaunch())
	p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(deleting, replacement)...))
	assert.Equal(t, consts.ReasonQuotaExceeded, nodeClaimCondition(t, p, 1).Reason)

	live := newWorkspaceNodeClaim(testNodeClaimName, status.Condition{Type: karpenterv1.ConditionTypeLaunched,
		Status: metav1.ConditionUnknown, Reason: "LaunchFailed", Message: "creating instance failed, TooManyRequests"})
	p = newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(deleting, live)...))
	assert.Equal(t, "LaunchFailed", nodeClaimCondition(t, p, 1).Reason)
}

// A capacity failure in one zone is not CapacityUnavailable while the provider tries the other zones, until it reports
// that no offering is available in any zone.
func TestProvisioningFailure_ZonalCapacityFailure(t *testing.T) {
	const zonalMessage = "unable to allocate resources in the selected zone (eastus-1). (will try a different zone to fulfill your request)"
	zonal := newWorkspaceNodeClaim(testNodeClaimName, status.Condition{Type: karpenterv1.ConditionTypeLaunched,
		Status: metav1.ConditionUnknown, Reason: "ZonalAllocationFailure", Message: zonalMessage})
	p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(zonal)...))
	nodeClaimCond := nodeClaimCondition(t, p, 1)
	assert.Equal(t, "ZonalAllocationFailure", nodeClaimCond.Reason)
	assert.Equal(t, zonalMessage, nodeClaimCond.Message)
	assert.False(t, consts.IsTerminatedReason(nodeClaimCond.Reason))

	ws := testWorkspace(1)
	ws.Status.Conditions = []metav1.Condition{{Type: string(kaitov1beta1.ConditionTypeNodeClaimStatus), Status: metav1.ConditionFalse,
		Reason: "ZonalAllocationFailure", Message: zonalMessage, ObservedGeneration: 1}}
	retry := newWorkspaceNodeClaim(testPoolName+"-hjklm", awaitingLaunch())
	p = newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(retry,
		failureEvent("ice", testNodeClaimName, corev1.EventTypeWarning, insufficientCapacityEventReason, noOfferingMessage, testNow))...))
	nodeClaimCond, _ = nodeClaimConditions(t, p, ws)
	assert.Equal(t, consts.ReasonCapacityUnavailable, nodeClaimCond.Reason)
	assert.Equal(t, noOfferingCapacityUnavailableMessage, nodeClaimCond.Message)
}

// An asynchronous Azure failure after launch is reported from its AsyncProvisioningError Event.
func TestProvisioningFailure_AsynchronousProvisioningError(t *testing.T) {
	replacement := newWorkspaceNodeClaim(testPoolName+"-hjklm", launchedOK(), awaitingRegistration())
	p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(replacement,
		failureEvent("async", testNodeClaimName, corev1.EventTypeWarning, asyncProvisioningEventReason, azureAllocationFailedError, testNow.Add(-time.Minute)),
		failureEvent("ice", testNodeClaimName, corev1.EventTypeWarning, insufficientCapacityEventReason, noOfferingMessage, testNow),
		failureEvent("normal", testNodeClaimName, corev1.EventTypeNormal, "Launched", "launched", testNow),
	)...))

	nodeClaimCond := nodeClaimCondition(t, p, 1)
	assert.Equal(t, consts.ReasonCapacityUnavailable, nodeClaimCond.Reason)
	assert.True(t, strings.HasPrefix(nodeClaimCond.Message, "AsyncProvisioningError: AllocationFailed: Allocation failed."), nodeClaimCond.Message)
}

// An exhausted regional quota is only reported by an InsufficientCapacityError Event.
func TestProvisioningFailure_RegionalQuotaEvent(t *testing.T) {
	p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(
		failureEvent("ice", testNodeClaimName, corev1.EventTypeWarning, insufficientCapacityEventReason, regionalQuotaMessage, testNow),
	)...))

	nodeClaimCond := nodeClaimCondition(t, p, 1)
	assert.Equal(t, consts.ReasonQuotaExceeded, nodeClaimCond.Reason)
	assert.True(t, strings.HasPrefix(nodeClaimCond.Message, "InsufficientCapacityError: NodeClaim "+testNodeClaimName), nodeClaimCond.Message)
}

// Once the provider caches the offering as unavailable after a regional quota error, the retries fail within seconds
// with no-offering errors, whose newer Events must not hide the quota error.
func TestProvisioningFailure_RegionalQuotaEventFollowedByNoOffering(t *testing.T) {
	p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(
		failureEvent("quota", testNodeClaimName, corev1.EventTypeWarning, insufficientCapacityEventReason, regionalQuotaMessage, testNow.Add(-time.Minute)),
		failureEvent("retry1", testPoolName+"-hjklm", corev1.EventTypeWarning, insufficientCapacityEventReason,
			strings.ReplaceAll(noOfferingMessage, testNodeClaimName, testPoolName+"-hjklm"), testNow.Add(-30*time.Second)),
		failureEvent("retry2", testPoolName+"-npqrs", corev1.EventTypeWarning, insufficientCapacityEventReason,
			strings.ReplaceAll(noOfferingMessage, testNodeClaimName, testPoolName+"-npqrs"), testNow),
	)...))

	nodeClaimCond := nodeClaimCondition(t, p, 1)
	assert.Equal(t, consts.ReasonQuotaExceeded, nodeClaimCond.Reason)
	assert.True(t, strings.HasPrefix(nodeClaimCond.Message, "InsufficientCapacityError: NodeClaim "+testNodeClaimName+" event: insufficient capacity, regional"), nodeClaimCond.Message)
}

// A NodeClass resolution error is read from the NodeClass instead, so its stale Events, once the NodeClass is
// restored, do not hide the errors of the current attempts.
func TestProvisioningFailure_NodeClassResolutionEventIsNotReported(t *testing.T) {
	resolution := "NodeClaim " + testPoolName + "-hjklm event: insufficient capacity, resolving node class, aksnodeclasses.karpenter.azure.com \"image-family-ubuntu\" not found"
	t.Run("with a no-offering error", func(t *testing.T) {
		p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(
			failureEvent("noOffering", testNodeClaimName, corev1.EventTypeWarning, insufficientCapacityEventReason, noOfferingMessage, testNow.Add(-time.Minute)),
			failureEvent("resolution", testPoolName+"-hjklm", corev1.EventTypeWarning, insufficientCapacityEventReason, resolution, testNow),
		)...))
		nodeClaimCond := nodeClaimCondition(t, p, 1)
		assert.Equal(t, consts.ReasonCapacityUnavailable, nodeClaimCond.Reason)
		assert.Equal(t, noOfferingCapacityUnavailableMessage, nodeClaimCond.Message)
	})
	t.Run("alone", func(t *testing.T) {
		p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(
			newWorkspaceNodeClaim(testPoolName+"-npqrs", awaitingLaunch()),
			failureEvent("resolution", testPoolName+"-hjklm", corev1.EventTypeWarning, insufficientCapacityEventReason, resolution, testNow),
		)...))
		nodeClaimCond := nodeClaimCondition(t, p, 1)
		assert.Equal(t, nodeclaim.NodeClaimNotReadyReason, nodeClaimCond.Reason)
	})
}

// A no-offering error does not replace the specific cause already reported for the current spec.
func TestProvisioningFailure_NoOfferingKeepsSpecificCause(t *testing.T) {
	newProvisioner := func() *KarpenterProvisioner {
		return newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(
			failureEvent("ice", testNodeClaimName, corev1.EventTypeWarning, insufficientCapacityEventReason, noOfferingMessage, testNow),
		)...))
	}
	withNodeClaimCondition := func(reason, message string, generation int64) *kaitov1beta1.Workspace {
		ws := testWorkspace(1)
		ws.Status.Conditions = []metav1.Condition{{Type: string(kaitov1beta1.ConditionTypeNodeClaimStatus), Status: metav1.ConditionFalse,
			Reason: reason, Message: message, ObservedGeneration: generation}}
		return ws
	}

	t.Run("no reported cause", func(t *testing.T) {
		nodeClaimCond, _ := nodeClaimConditions(t, newProvisioner(), testWorkspace(1))
		assert.Equal(t, consts.ReasonCapacityUnavailable, nodeClaimCond.Reason)
		assert.Equal(t, noOfferingCapacityUnavailableMessage, nodeClaimCond.Message)
	})
	t.Run("reported specific cause is kept", func(t *testing.T) {
		ws := withNodeClaimCondition(consts.ReasonQuotaExceeded, "CreateInstanceFailed: quota (last reconcile error: boom)", 1)
		nodeClaimCond, _ := nodeClaimConditions(t, newProvisioner(), ws)
		assert.Equal(t, consts.ReasonQuotaExceeded, nodeClaimCond.Reason)
		assert.Equal(t, "CreateInstanceFailed: quota", nodeClaimCond.Message)
	})
	t.Run("reported cause of an older spec", func(t *testing.T) {
		ws := withNodeClaimCondition(consts.ReasonQuotaExceeded, "CreateInstanceFailed: quota", 0)
		nodeClaimCond, _ := nodeClaimConditions(t, newProvisioner(), ws)
		assert.Equal(t, consts.ReasonCapacityUnavailable, nodeClaimCond.Reason)
	})
	t.Run("reported progress", func(t *testing.T) {
		ws := withNodeClaimCondition(nodeclaim.NodeClaimNotReadyReason, "provisioning", 1)
		nodeClaimCond, _ := nodeClaimConditions(t, newProvisioner(), ws)
		assert.Equal(t, consts.ReasonCapacityUnavailable, nodeClaimCond.Reason)
	})
}

// A specific failure outlives the NodeClaim that carried it while the retries fail before launching.
func TestProvisioningFailure_SpecificCauseOutlivesItsNodeClaim(t *testing.T) {
	awaiting := newWorkspaceNodeClaim(testPoolName+"-hjklm", awaitingLaunch())
	booting := newWorkspaceNodeClaim(testPoolName+"-hjklm", launchedAt(time.Minute), awaitingRegistration())
	ready := newWorkspaceNodeClaim(testNodeClaimName, launchedAt(time.Hour), registeredOK(), initializedOK(), readyOK())
	testcases := map[string]struct {
		objs             []client.Object
		targetNodeCount  int32
		reportedGen      int64
		expectedReason   string
		expectedProgress bool
	}{
		"retry not launched yet":                  {objs: []client.Object{awaiting}, targetNodeCount: 1, reportedGen: 1, expectedReason: consts.ReasonQuotaExceeded},
		"no NodeClaim":                            {targetNodeCount: 1, reportedGen: 1, expectedReason: consts.ReasonQuotaExceeded},
		"another replica is ready":                {objs: []client.Object{ready, awaiting}, targetNodeCount: 2, reportedGen: 1, expectedReason: consts.ReasonQuotaExceeded},
		"a new attempt launched":                  {objs: []client.Object{booting}, targetNodeCount: 1, reportedGen: 1, expectedReason: nodeclaim.NodeClaimNotReadyReason, expectedProgress: true},
		"the cause was reported for another spec": {objs: []client.Object{awaiting}, targetNodeCount: 1, reportedGen: 0, expectedReason: nodeclaim.NodeClaimNotReadyReason, expectedProgress: true},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(tc.objs...)...))
			ws := testWorkspace(tc.targetNodeCount)
			ws.Status.Conditions = []metav1.Condition{{Type: string(kaitov1beta1.ConditionTypeNodeClaimStatus), Status: metav1.ConditionFalse,
				Reason: consts.ReasonQuotaExceeded, Message: "CreateInstanceFailed: quota", ObservedGeneration: tc.reportedGen}}

			nodeClaimCond, _ := nodeClaimConditions(t, p, ws)
			assert.Equal(t, tc.expectedReason, nodeClaimCond.Reason)
			if !tc.expectedProgress {
				assert.Equal(t, "CreateInstanceFailed: quota", nodeClaimCond.Message)
			}
		})
	}
}

// An invalid NodeClass or NodePool is only reported while it lasts, a no-offering error is refreshed by a newer one
// but still kept over progress, and an unclassified failure is not kept.
func TestProvisioningFailure_ReportedCauseOfFixedOrNoOfferingFailure(t *testing.T) {
	noOfferingEvent := failureEvent("ice", testNodeClaimName, corev1.EventTypeWarning, insufficientCapacityEventReason, noOfferingMessage, testNow)
	awaiting := newWorkspaceNodeClaim(testNodeClaimName, awaitingLaunch())
	const (
		nodeClassMissing           = `NodeClass "image-family-ubuntu" is not ready (NotFound): NodeClass does not exist`
		nodePoolInvalid            = "invalid requirements"
		authorizationFailed        = "CreateInstanceFailed: AuthorizationFailed: the client does not have authorization"
		olderNoOfferingUnavailable = insufficientCapacityEventReason + ": NodeClaim " + testPoolName + "-hjklm event: insufficient capacity, all requested instance types were unavailable during launch"
	)
	testcases := map[string]struct {
		objs            []client.Object
		reportedReason  string
		reportedMessage string
		expectedReason  string
		expectedMessage string
	}{
		"fixed NodeClass, retries fail with a no-offering error": {
			objs:           []client.Object{noOfferingEvent},
			reportedReason: consts.ReasonNodeClassInvalid, reportedMessage: nodeClassMissing + " (last reconcile error: boom)",
			expectedReason: consts.ReasonCapacityUnavailable, expectedMessage: noOfferingCapacityUnavailableMessage,
		},
		"fixed NodeClass, retry awaiting launch": {
			objs:           []client.Object{awaiting},
			reportedReason: consts.ReasonNodeClassInvalid, reportedMessage: nodeClassMissing,
			expectedReason: nodeclaim.NodeClaimNotReadyReason,
		},
		"fixed NodePool, retries fail with a no-offering error": {
			objs:           []client.Object{noOfferingEvent},
			reportedReason: consts.ReasonNodePoolInvalid, reportedMessage: nodePoolInvalid,
			expectedReason: consts.ReasonCapacityUnavailable, expectedMessage: noOfferingCapacityUnavailableMessage,
		},
		"NodeClaim configuration failure is kept over a no-offering error": {
			objs:           []client.Object{noOfferingEvent},
			reportedReason: consts.ReasonNodeConfigurationInvalid, reportedMessage: authorizationFailed,
			expectedReason: consts.ReasonNodeConfigurationInvalid, expectedMessage: authorizationFailed,
		},
		"newer no-offering error refreshes the message": {
			objs:           []client.Object{noOfferingEvent},
			reportedReason: consts.ReasonCapacityUnavailable, reportedMessage: olderNoOfferingUnavailable,
			expectedReason: consts.ReasonCapacityUnavailable, expectedMessage: noOfferingCapacityUnavailableMessage,
		},
		"no-offering error is kept over progress": {
			objs:           []client.Object{awaiting},
			reportedReason: consts.ReasonCapacityUnavailable, reportedMessage: olderNoOfferingUnavailable,
			expectedReason: consts.ReasonCapacityUnavailable, expectedMessage: olderNoOfferingUnavailable,
		},
		"unclassified failure is not kept": {
			objs:           []client.Object{awaiting},
			reportedReason: "LaunchFailed", reportedMessage: "creating instance failed, TooManyRequests",
			expectedReason: nodeclaim.NodeClaimNotReadyReason,
		},
		"unclassified insufficient capacity error is not kept": {
			objs:           []client.Object{awaiting},
			reportedReason: insufficientCapacityEventReason, reportedMessage: "NodeClaim " + testPoolName + "-hjklm event: insufficient capacity, resolving node class, not found",
			expectedReason: nodeclaim.NodeClaimNotReadyReason,
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(tc.objs...)...))
			ws := testWorkspace(1)
			ws.Status.Conditions = []metav1.Condition{{Type: string(kaitov1beta1.ConditionTypeNodeClaimStatus), Status: metav1.ConditionFalse,
				Reason: tc.reportedReason, Message: tc.reportedMessage, ObservedGeneration: 1}}

			nodeClaimCond, _ := nodeClaimConditions(t, p, ws)
			assert.Equal(t, tc.expectedReason, nodeClaimCond.Reason)
			if tc.expectedMessage != "" {
				assert.Equal(t, tc.expectedMessage, nodeClaimCond.Message)
			}
		})
	}
}

// Events are only reported for the NodePool's own NodeClaims, within the window and after the NodePool was created.
func TestProvisioningFailure_EventScope(t *testing.T) {
	testcases := map[string]struct {
		nodePool *karpenterv1.NodePool
		event    *corev1.Event
	}{
		"sibling NodePool": {event: failureEvent("ice", testPoolName+"-a-bcdfg", corev1.EventTypeWarning,
			insufficientCapacityEventReason, regionalQuotaMessage, testNow)},
		"outside the window": {event: failureEvent("ice", testNodeClaimName, corev1.EventTypeWarning,
			insufficientCapacityEventReason, regionalQuotaMessage, testNow.Add(-nodeClaimEventWindow-time.Second))},
		"before the NodePool was created": {
			nodePool: &karpenterv1.NodePool{ObjectMeta: metav1.ObjectMeta{Name: testPoolName, CreationTimestamp: metav1.NewTime(testNow.Add(-time.Minute))}},
			event: failureEvent("ice", testNodeClaimName, corev1.EventTypeWarning,
				insufficientCapacityEventReason, regionalQuotaMessage, testNow.Add(-2*time.Minute)),
		},
		"another reason": {event: failureEvent("taint", testNodeClaimName, corev1.EventTypeWarning,
			"UnregisteredTaintMissing", "missing taint", testNow)},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			nodePool := tc.nodePool
			if nodePool == nil {
				nodePool = testNodePool()
			}
			booting := newWorkspaceNodeClaim(testNodeClaimName, launchedOK(), awaitingRegistration())
			p := newFailureTestProvisioner(newFailureTestClient(nil, makeNodeClassUnstructured(testConfig.DefaultName), nodePool, booting, tc.event))

			nodeClaimCond := nodeClaimCondition(t, p, 1)
			assert.Equal(t, nodeclaim.NodeClaimNotReadyReason, nodeClaimCond.Reason)
			assert.Equal(t, "NodeClaim Registered=Unknown (NodeNotFound): Node not registered with cluster", nodeClaimCond.Message)
		})
	}
}

// Without a NodePool, there are no NodeClaim Events to look up.
func TestProvisioningFailure_MissingNodePool(t *testing.T) {
	counter := &eventListCounter{}
	p := newFailureTestProvisioner(newFailureTestClient(counter, makeNodeClassUnstructured(testConfig.DefaultName),
		failureEvent("ice", testNodeClaimName, corev1.EventTypeWarning, insufficientCapacityEventReason, regionalQuotaMessage, testNow)))

	nodeClaimCond := nodeClaimCondition(t, p, 1)
	assert.Equal(t, "NodeClaimNotReady", nodeClaimCond.Reason)
	assert.Equal(t, "Ready NodeClaims are not enough", nodeClaimCond.Message)
	assert.Zero(t, counter.calls)
}

func TestProvisioningFailure_NodePoolConditions(t *testing.T) {
	invalid := status.Condition{Type: karpenterv1.ConditionTypeValidationSucceeded, Status: metav1.ConditionFalse,
		Reason: "NodePoolValidationFailed", Message: "invalid requirement"}
	unhealthy := status.Condition{Type: karpenterv1.ConditionTypeNodeRegistrationHealthy, Status: metav1.ConditionFalse,
		Reason: "RegistrationFailed", Message: "Failed to register node"}
	quotaUnhealthy := status.Condition{Type: karpenterv1.ConditionTypeNodeRegistrationHealthy, Status: metav1.ConditionFalse,
		Reason: "CreateInstanceFailed", Message: quotaLaunchMessage}
	stuck := newWorkspaceNodeClaim(testNodeClaimName, launchedAt(stuckRegistrationThreshold+time.Minute), awaitingRegistration())
	booting := newWorkspaceNodeClaim(testNodeClaimName, launchedAt(stuckRegistrationThreshold-time.Minute), awaitingRegistration())

	testcases := map[string]struct {
		nodePool        *karpenterv1.NodePool
		objs            []client.Object
		expectedReason  string
		expectedMessage string
	}{
		"invalid NodePool blocks every launch": {
			nodePool:        testNodePool(invalid, unhealthy),
			objs:            []client.Object{newWorkspaceNodeClaim(testNodeClaimName, quotaLaunchFailure())},
			expectedReason:  consts.ReasonNodePoolInvalid,
			expectedMessage: "invalid requirement",
		},
		"invalid NodePool without a message": {
			nodePool: testNodePool(status.Condition{Type: karpenterv1.ConditionTypeValidationSucceeded, Status: metav1.ConditionFalse,
				Reason: "NodePoolValidationFailed"}),
			expectedReason:  consts.ReasonNodePoolInvalid,
			expectedMessage: "NodePoolValidationFailed",
		},
		"failed registrations while the current node does not register": {
			nodePool:        testNodePool(unhealthy),
			objs:            []client.Object{stuck},
			expectedReason:  consts.ReasonNodeBootstrapFailed,
			expectedMessage: "RegistrationFailed: Failed to register node",
		},
		"failed launches while the current node does not register": {
			nodePool:        testNodePool(quotaUnhealthy),
			objs:            []client.Object{stuck},
			expectedReason:  consts.ReasonQuotaExceeded,
			expectedMessage: "CreateInstanceFailed: " + quotaLaunchMessage,
		},
		"stale failed registrations while the current node boots": {
			nodePool:        testNodePool(unhealthy),
			objs:            []client.Object{booting},
			expectedReason:  nodeclaim.NodeClaimNotReadyReason,
			expectedMessage: "NodeClaim Registered=Unknown (NodeNotFound): Node not registered with cluster",
		},
		"stale failed registrations without a launched NodeClaim": {
			nodePool:        testNodePool(unhealthy),
			objs:            []client.Object{newWorkspaceNodeClaim(testNodeClaimName, awaitingLaunch())},
			expectedReason:  nodeclaim.NodeClaimNotReadyReason,
			expectedMessage: "NodeClaim Launched=Unknown (AwaitingReconciliation): object is awaiting reconciliation",
		},
		"Events come before failed registrations": {
			nodePool: testNodePool(unhealthy),
			objs: []client.Object{stuck, failureEvent("ice", testPoolName+"-hjklm", corev1.EventTypeWarning,
				insufficientCapacityEventReason, noOfferingMessage, testNow)},
			expectedReason:  consts.ReasonCapacityUnavailable,
			expectedMessage: noOfferingCapacityUnavailableMessage,
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			objs := append([]client.Object{makeNodeClassUnstructured(testConfig.DefaultName), tc.nodePool}, tc.objs...)
			p := newFailureTestProvisioner(newFailureTestClient(nil, objs...))

			nodeClaimCond := nodeClaimCondition(t, p, 1)
			assert.Equal(t, tc.expectedReason, nodeClaimCond.Reason)
			assert.Equal(t, tc.expectedMessage, nodeClaimCond.Message)
		})
	}
}

// The NodeClaim Events are listed at most once per interval for all NodePools, also when the list fails.
func TestProvisioningFailure_EventListInterval(t *testing.T) {
	counter := &eventListCounter{}
	c := newFailureTestClient(counter, baseObjects(
		failureEvent("first", testNodeClaimName, corev1.EventTypeWarning, insufficientCapacityEventReason, noOfferingMessage, testNow),
	)...)
	p := newFailureTestProvisioner(c)
	now := testNow
	p.now = func() time.Time { return now }

	assert.Equal(t, consts.ReasonCapacityUnavailable, nodeClaimCondition(t, p, 1).Reason)
	assert.Equal(t, 1, counter.calls)
	assert.Equal(t, []string{"involvedObject.kind=NodeClaim,type=Warning"}, counter.selectors)

	now = testNow.Add(time.Minute)
	require.NoError(t, c.Create(context.Background(), failureEvent("second", testNodeClaimName, corev1.EventTypeWarning,
		asyncProvisioningEventReason, "Failed to register: ERROR CODE: ImageNotFound", now)))
	assert.Equal(t, consts.ReasonCapacityUnavailable, nodeClaimCondition(t, p, 1).Reason)
	assert.Equal(t, 1, counter.calls, "Events are not listed again within the interval")

	now = testNow.Add(nodeClaimEventListInterval)
	assert.Equal(t, consts.ReasonNodeImageInvalid, nodeClaimCondition(t, p, 1).Reason)
	assert.Equal(t, 2, counter.calls)

	counter.err = errors.New("connection refused")
	now = testNow.Add(2 * nodeClaimEventListInterval)
	assert.Equal(t, consts.ReasonNodeImageInvalid, nodeClaimCondition(t, p, 1).Reason, "a failed list keeps the previous Events")
	assert.Equal(t, 3, counter.calls)
	now = now.Add(time.Minute)
	nodeClaimCondition(t, p, 1)
	assert.Equal(t, 3, counter.calls, "a failed list is not retried within the interval")
}

// A launched NodeClaim whose node does not register yet, or does not initialize, is progress.
func TestProvisioningFailure_Progress(t *testing.T) {
	initializing := newWorkspaceNodeClaim(testNodeClaimName, launchedOK(), registeredOK(), status.Condition{
		Type: karpenterv1.ConditionTypeInitialized, Status: metav1.ConditionUnknown, Reason: "ResourceNotRegistered",
		Message: `Resource "nvidia.com/gpu" was requested but not registered`})
	p := newFailureTestProvisioner(newFailureTestClient(nil, baseObjects(initializing)...))

	nodeClaimCond := nodeClaimCondition(t, p, 1)
	assert.Equal(t, nodeclaim.NodeClaimNotReadyReason, nodeClaimCond.Reason)
	assert.Equal(t, `NodeClaim Initialized=Unknown (ResourceNotRegistered): Resource "nvidia.com/gpu" was requested but not registered`, nodeClaimCond.Message)
}

func makeNodeClass(name, readyStatus, reason, message string) *unstructured.Unstructured {
	obj := makeNodeClassUnstructured(name)
	obj.Object["status"] = map[string]interface{}{
		"conditions": []interface{}{
			map[string]interface{}{"type": "Ready", "status": readyStatus, "reason": reason, "message": message},
		},
	}
	return obj
}

func makeNodeClassWithDependents(name string, conditions ...map[string]interface{}) *unstructured.Unstructured {
	obj := makeNodeClassUnstructured(name)
	conds := make([]interface{}, 0, len(conditions))
	for _, c := range conditions {
		conds = append(conds, c)
	}
	obj.Object["status"] = map[string]interface{}{"conditions": conds}
	return obj
}

func nodeClassWithoutStatus() *unstructured.Unstructured {
	obj := makeNodeClassUnstructured(testConfig.DefaultName)
	delete(obj.Object, "status")
	return obj
}

func TestProvisioningFailure_NodeClass(t *testing.T) {
	failing := newWorkspaceNodeClaim(testNodeClaimName, quotaLaunchFailure())
	awaiting := newWorkspaceNodeClaim(testNodeClaimName, awaitingLaunch())
	notReconciled := makeNodeClass(testConfig.DefaultName, "Unknown", "AwaitingReconciliation", "object is awaiting reconciliation")
	testcases := map[string]struct {
		nodeClass       client.Object
		nodeClaim       client.Object
		expectedReason  string
		expectedMessage string
	}{
		"missing": {
			nodeClaim:       failing,
			expectedReason:  consts.ReasonNodeClassInvalid,
			expectedMessage: `NodeClass "image-family-ubuntu" is not ready (NotFound): NodeClass does not exist`,
		},
		"Ready=False": {
			nodeClass:       makeNodeClass(testConfig.DefaultName, "False", "ImagesNotReady", "failed to resolve images"),
			nodeClaim:       failing,
			expectedReason:  consts.ReasonNodeClassInvalid,
			expectedMessage: `NodeClass "image-family-ubuntu" is not ready (ImagesNotReady): failed to resolve images`,
		},
		"Ready=False reports the failed dependent conditions": {
			nodeClass: makeNodeClassWithDependents(testConfig.DefaultName,
				map[string]interface{}{"type": "Ready", "status": "False", "reason": "UnhealthyDependents", "message": "SubnetsReady=False"},
				map[string]interface{}{"type": "ImagesReady", "status": "True", "reason": "ImagesReady"},
				map[string]interface{}{"type": "SubnetsReady", "status": "False", "reason": "SubnetIDInvalid", "message": "vnetSubnetID does not match the cluster virtual network"},
			),
			nodeClaim:       failing,
			expectedReason:  consts.ReasonNodeClassInvalid,
			expectedMessage: `NodeClass "image-family-ubuntu" is not ready (UnhealthyDependents): SubnetsReady=False (SubnetIDInvalid): vnetSubnetID does not match the cluster virtual network`,
		},
		"Ready=Unknown is progress": {
			nodeClass:       notReconciled,
			nodeClaim:       awaiting,
			expectedReason:  nodeclaim.NodeClaimNotReadyReason,
			expectedMessage: `NodeClass "image-family-ubuntu" is not ready (AwaitingReconciliation): object is awaiting reconciliation`,
		},
		"Ready=Unknown comes after a NodeClaim failure": {
			nodeClass:       notReconciled,
			nodeClaim:       failing,
			expectedReason:  consts.ReasonQuotaExceeded,
			expectedMessage: "CreateInstanceFailed: " + quotaLaunchMessage,
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			objs := []client.Object{testNodePool(), tc.nodeClaim}
			if tc.nodeClass != nil {
				objs = append(objs, tc.nodeClass)
			}
			p := newFailureTestProvisioner(newFailureTestClient(nil, objs...))

			nodeClaimCond := nodeClaimCondition(t, p, 1)
			assert.Equal(t, tc.expectedReason, nodeClaimCond.Reason)
			assert.Equal(t, tc.expectedMessage, nodeClaimCond.Message)
		})
	}
}

func TestCheckNodeClassReady(t *testing.T) {
	testcases := map[string]struct {
		objs           []client.Object
		expectedStatus metav1.ConditionStatus
		expectedReason string
		expectedFailed bool
	}{
		"missing":             {expectedReason: "NotFound", expectedFailed: true},
		"no status":           {objs: []client.Object{nodeClassWithoutStatus()}, expectedStatus: metav1.ConditionUnknown, expectedReason: "NotReady"},
		"Ready=False":         {objs: []client.Object{makeNodeClass(testConfig.DefaultName, "False", "SubnetsNotReady", "subnet not found")}, expectedStatus: metav1.ConditionFalse, expectedReason: "SubnetsNotReady", expectedFailed: true},
		"Ready=Unknown":       {objs: []client.Object{makeNodeClass(testConfig.DefaultName, "Unknown", "", "")}, expectedStatus: metav1.ConditionUnknown, expectedReason: "NotReady"},
		"Ready=False no info": {objs: []client.Object{makeNodeClass(testConfig.DefaultName, "False", "", "")}, expectedStatus: metav1.ConditionFalse, expectedReason: "NotReady", expectedFailed: true},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			p := NewKarpenterProvisioner(newFakeClient(tc.objs...), testConfig)
			err := p.checkNodeClassReady(context.Background(), testConfig.DefaultName)
			var notReady *NodeClassNotReadyError
			require.ErrorAs(t, err, &notReady)
			assert.Equal(t, tc.expectedStatus, notReady.Status)
			assert.Equal(t, tc.expectedReason, notReady.Reason)
			assert.Equal(t, tc.expectedFailed, notReady.Failed())
		})
	}

	t.Run("ready", func(t *testing.T) {
		p := NewKarpenterProvisioner(newFakeClient(makeNodeClassUnstructured(testConfig.DefaultName)), testConfig)
		assert.NoError(t, p.checkNodeClassReady(context.Background(), testConfig.DefaultName))
	})

	t.Run("API error is not a NotReady error", func(t *testing.T) {
		c := newFakeClientWithInterceptors(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return errors.New("connection refused")
			},
		})
		p := NewKarpenterProvisioner(c, testConfig)
		err := p.checkNodeClassReady(context.Background(), testConfig.DefaultName)
		var notReady *NodeClassNotReadyError
		assert.Error(t, err)
		assert.False(t, errors.As(err, &notReady))
	})
}

func TestClassifyProvisioningFailure(t *testing.T) {
	testcases := map[string]struct {
		reason   string
		message  string
		expected string
	}{
		"Azure SubscriptionQuotaReached":         {reason: "SubscriptionQuotaReached", message: "subscription level on-demand vCPU quota for X has been reached", expected: consts.ReasonQuotaExceeded},
		"older Azure SKU family quota":           {reason: "CreateInstanceFailed", message: quotaLaunchMessage, expected: consts.ReasonQuotaExceeded},
		"spot quota":                             {reason: "CreateInstanceFailed", message: "this subscription has reached the regional vCPU quota for spot (LowPriorityQuota)", expected: consts.ReasonQuotaExceeded},
		"regional quota Event":                   {reason: insufficientCapacityEventReason, message: regionalQuotaMessage, expected: consts.ReasonQuotaExceeded},
		"quota error code":                       {reason: "CreateInstanceFailed", message: "ERROR CODE: OperationNotAllowed\nOperation could not be completed as it results in exceeding approved Total Regional Cores quota", expected: consts.ReasonQuotaExceeded},
		"Azure AllocationFailure":                {reason: "AllocationFailure", message: "unable to allocate resources with selected VM size (X)", expected: consts.ReasonCapacityUnavailable},
		"Azure SKUNotAvailable":                  {reason: "SKUNotAvailable", message: "VM size X is not supported for this subscription in this location", expected: consts.ReasonCapacityUnavailable},
		"older Azure zonal allocation":           {reason: "CreateInstanceFailed", message: "unable to allocate resources in the selected zone (1). (will try a different zone to fulfill your request)", expected: ""},
		"Azure ZonalAllocationFailure":           {reason: "ZonalAllocationFailure", message: "unable to allocate resources in the selected zone (eastus-1). (will try a different zone to fulfill your request)", expected: ""},
		"Azure overconstrained zonal allocation": {reason: "OverconstrainedZonalAllocationFailure", message: "unable to allocate resources in the selected zone (eastus-1) with on-demand capacity type and X VM size.", expected: ""},
		"Azure overconstrained allocation":       {reason: "OverconstrainedAllocationFailure", message: "unable to allocate resources in all zones with on-demand capacity type and X VM size.", expected: consts.ReasonCapacityUnavailable},
		"asynchronous zonal allocation failure":  {reason: asyncProvisioningEventReason, message: "Failed to register: unable to allocate resources in the selected zone (1). (will try a different zone to fulfill your request)", expected: consts.ReasonCapacityUnavailable},
		"older Azure SKU not available":          {reason: "CreateInstanceFailed", message: "the requested SKU is unavailable for instance type X in zone 1 with capacity type spot", expected: consts.ReasonCapacityUnavailable},
		"asynchronous allocation failure":        {reason: asyncProvisioningEventReason, message: azureAllocationFailedError, expected: consts.ReasonCapacityUnavailable},
		"AKS machine overconstrained allocation": {reason: asyncProvisioningEventReason, message: "Failed to register: failed to create AKS machine \"m\" during LRO, unhandled provisioning error: code=OverconstrainedAllocationRequest, message=no capacity", expected: consts.ReasonCapacityUnavailable},
		"VM size not supported":                  {reason: "CreateInstanceFailed", message: "ERROR CODE: VMSizeNotSupported", expected: consts.ReasonCapacityUnavailable},
		"image not found":                        {reason: "CreateInstanceFailed", message: "ERROR CODE: ImageNotFound\nThe platform image 'x' is not available", expected: consts.ReasonNodeImageInvalid},
		"gallery image not found":                {reason: "CreateInstanceFailed", message: `{"code": "GalleryImageNotFound", "message": "not found"}`, expected: consts.ReasonNodeImageInvalid},
		"invalid image reference":                {reason: "CreateInstanceFailed", message: "ERROR CODE: InvalidParameter\nThe value of parameter imageReference is invalid.", expected: consts.ReasonNodeImageInvalid},
		"other invalid parameter":                {reason: "CreateInstanceFailed", message: "ERROR CODE: InvalidParameter\nThe value of parameter osDisk is invalid.", expected: ""},
		"invalid image reference version":        {reason: "CreateInstanceFailed", message: `{"code": "InvalidParameter", "message": "The image version is invalid.", "target": "imageReference.version"}`, expected: consts.ReasonNodeImageInvalid},
		"other parameter mentioning the image":   {reason: "CreateInstanceFailed", message: "ERROR CODE: InvalidParameter\nThe value of parameter securityProfile.securityType is invalid. The image referenced by imageReference does not support it.", expected: ""},
		"CSE failure":                            {reason: asyncProvisioningEventReason, message: "Failed to register: creating VM CSE for VM \"v\": ERROR CODE: VMExtensionProvisioningError", expected: consts.ReasonNodeBootstrapFailed},
		"registration timeouts":                  {reason: registrationFailedReason, message: "Failed to register node", expected: consts.ReasonNodeBootstrapFailed},
		"authorization":                          {reason: "CreateInstanceFailed", message: "ERROR CODE: LinkedAuthorizationFailed", expected: consts.ReasonNodeConfigurationInvalid},
		"subnet full":                            {reason: "CreateInstanceFailed", message: "ERROR CODE: SubnetIsFull", expected: consts.ReasonNodeConfigurationInvalid},
		"Azure Policy denial":                    {reason: "CreateInstanceFailed", message: "ERROR CODE: RequestDisallowedByPolicy\nResource 'x' was disallowed by policy.", expected: consts.ReasonNodeConfigurationInvalid},
		"resource provider not registered":       {reason: "CreateInstanceFailed", message: `{"code": "MissingSubscriptionRegistration", "message": "The subscription is not registered to use namespace"}`, expected: consts.ReasonNodeConfigurationInvalid},
		"missing referenced resource":            {reason: "CreateInstanceFailed", message: "ERROR CODE: InvalidResourceReference\nResource /subscriptions/0/subnets/x referenced by resource y was not found.", expected: consts.ReasonNodeConfigurationInvalid},
		"no compatible image":                    {reason: "CreateInstanceFailed", message: "getting launch template: no compatible images found for instance type Standard_NC24ads_A100_v4", expected: consts.ReasonNodeImageInvalid},
		"internal server error":                  {reason: "CreateInstanceFailed", message: "ERROR CODE: InternalServerError", expected: ""},
		"no offering":                            {reason: insufficientCapacityEventReason, message: noOfferingMessage, expected: consts.ReasonCapacityUnavailable},
		"no instance types available":            {reason: insufficientCapacityEventReason, message: "NodeClaim x event: insufficient capacity, no instance types available", expected: consts.ReasonCapacityUnavailable},
		"NodeClass not found":                    {reason: insufficientCapacityEventReason, message: "NodeClaim x event: insufficient capacity, resolving node class, not found", expected: ""},
		"AWS NodeClass not found":                {reason: insufficientCapacityEventReason, message: "NodeClaim x event: insufficient capacity, resolving nodeclass, not found", expected: ""},
		"AWS fleet error":                        {reason: insufficientCapacityEventReason, message: "NodeClaim x event: insufficient capacity, with fleet error(s), InsufficientInstanceCapacity: no capacity", expected: consts.ReasonCapacityUnavailable},
		"unknown insufficient capacity error":    {reason: insufficientCapacityEventReason, message: "NodeClaim x event: insufficient capacity, a future provider error", expected: consts.ReasonCapacityUnavailable},
		"unknown error of another reason":        {reason: "CreateInstanceFailed", message: "a future provider error", expected: ""},
		"throttling":                             {reason: "CreateInstanceFailed", message: "ERROR CODE: TooManyRequests", expected: ""},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, classifyProvisioningFailure(tc.reason, tc.message))
		})
	}
}

func TestIsNodePoolNodeClaim(t *testing.T) {
	longPool := strings.Repeat("a", 60)
	testcases := map[string]struct {
		nodeClaim string
		nodePool  string
		expected  bool
	}{
		"generated name":            {nodeClaim: "default-ws1-bcdfg", nodePool: testPoolName, expected: true},
		"sibling NodePool":          {nodeClaim: "default-ws1-a-bcdfg", nodePool: testPoolName},
		"short suffix":              {nodeClaim: "default-ws1-bcdf", nodePool: testPoolName},
		"other NodePool":            {nodeClaim: "default-ws10-bcdfg", nodePool: testPoolName},
		"truncated generate prefix": {nodeClaim: longPool[:maxGeneratedNameBaseLen] + "bcdfg", nodePool: longPool, expected: true},
		"untruncated long prefix":   {nodeClaim: longPool + "-bcdfg", nodePool: longPool},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isNodePoolNodeClaim(tc.nodeClaim, tc.nodePool))
		})
	}
}

func TestNodePoolFailureEvent(t *testing.T) {
	since := testNow.Add(-nodeClaimEventWindow)
	ice := func(at time.Time, message string) nodeClaimEvent {
		return nodeClaimEvent{nodeClaimName: testNodeClaimName, reason: insufficientCapacityEventReason, message: message, lastSeen: at}
	}
	async := nodeClaimEvent{nodeClaimName: testNodeClaimName, reason: asyncProvisioningEventReason, message: "async", lastSeen: testNow.Add(-3 * time.Minute)}

	best, ok := nodePoolFailureEvent([]nodeClaimEvent{ice(testNow, "newer"), async, ice(testNow.Add(-time.Minute), "older")}, testPoolName, since)
	require.True(t, ok)
	assert.Equal(t, "async", best.message, "an asynchronous provisioning error outranks insufficient capacity errors")

	best, ok = nodePoolFailureEvent([]nodeClaimEvent{ice(testNow.Add(-time.Minute), "older"), ice(testNow, "newer")}, testPoolName, since)
	require.True(t, ok)
	assert.Equal(t, "newer", best.message, "the latest Event of a tier is reported")

	best, ok = nodePoolFailureEvent([]nodeClaimEvent{ice(testNow.Add(-time.Minute), regionalQuotaMessage), ice(testNow, noOfferingMessage)}, testPoolName, since)
	require.True(t, ok)
	assert.Equal(t, regionalQuotaMessage, best.message, "an insufficient capacity error that says why outranks newer no-offering ones")

	best, ok = nodePoolFailureEvent([]nodeClaimEvent{ice(testNow.Add(-time.Minute), regionalQuotaMessage), ice(testNow, noOfferingMessage), async}, testPoolName, since)
	require.True(t, ok)
	assert.Equal(t, "async", best.message, "an asynchronous provisioning error outranks every insufficient capacity error")

	_, ok = nodePoolFailureEvent([]nodeClaimEvent{ice(since, "at since")}, testPoolName, since)
	assert.False(t, ok)
}

func TestEventLastSeen(t *testing.T) {
	e := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(testNow.Add(-time.Hour))},
		FirstTimestamp: metav1.NewTime(testNow.Add(-30 * time.Minute)),
		LastTimestamp:  metav1.NewTime(testNow.Add(-10 * time.Minute)),
	}
	assert.Equal(t, testNow.Add(-10*time.Minute), eventLastSeen(e))
	e.Series = &corev1.EventSeries{LastObservedTime: metav1.NewMicroTime(testNow.Add(-time.Minute))}
	assert.Equal(t, testNow.Add(-time.Minute), eventLastSeen(e))
}

func TestHasUnregisteredLaunch(t *testing.T) {
	testcases := map[string]struct {
		nodeClaim *karpenterv1.NodeClaim
		expected  bool
	}{
		"launched at the threshold":   {nodeClaim: newWorkspaceNodeClaim(testNodeClaimName, launchedAt(stuckRegistrationThreshold), awaitingRegistration()), expected: true},
		"launched within threshold":   {nodeClaim: newWorkspaceNodeClaim(testNodeClaimName, launchedAt(stuckRegistrationThreshold-time.Second), awaitingRegistration())},
		"registered":                  {nodeClaim: newWorkspaceNodeClaim(testNodeClaimName, launchedAt(time.Hour), registeredOK())},
		"not launched":                {nodeClaim: newWorkspaceNodeClaim(testNodeClaimName, awaitingLaunch())},
		"deleting":                    {nodeClaim: deletingNodeClaim(newWorkspaceNodeClaim(testNodeClaimName, launchedAt(time.Hour), awaitingRegistration()))},
		"no Registered condition yet": {nodeClaim: newWorkspaceNodeClaim(testNodeClaimName, launchedAt(time.Hour)), expected: true},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, hasUnregisteredLaunch([]*karpenterv1.NodeClaim{tc.nodeClaim}, testNow, stuckRegistrationThreshold))
		})
	}
}
