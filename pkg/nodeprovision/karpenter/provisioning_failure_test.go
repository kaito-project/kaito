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
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
)

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

const regionalQuotaMessage = "NodeClaim nc-1 event: regional on-demand vCPU quota limit for subscription has been reached. " +
	"To scale beyond this limit, please review the quota increase process here: https://learn.microsoft.com/en-us/azure/quotas/regional-quota-requests"

// eventListCounter counts Event list calls and records their field selectors.
type eventListCounter struct {
	calls     int
	selectors []string
}

// newFailureTestClient builds a fake client supporting the NodeClaim Event field selectors.
func newFailureTestClient(counter *eventListCounter, objs ...client.Object) client.Client {
	b := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(objs...).
		WithIndex(&corev1.Event{}, EventFieldInvolvedObjectUID, func(o client.Object) []string {
			return []string{string(o.(*corev1.Event).InvolvedObject.UID)}
		}).
		WithIndex(&corev1.Event{}, EventFieldType, func(o client.Object) []string {
			return []string{o.(*corev1.Event).Type}
		})
	if counter != nil {
		b = b.WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.EventList); ok {
					counter.calls++
					lo := &client.ListOptions{}
					lo.ApplyOptions(opts)
					if lo.Namespace != nodeClaimEventNamespace {
						return errors.New("NodeClaim Events must be listed in the default namespace")
					}
					if lo.FieldSelector != nil {
						counter.selectors = append(counter.selectors, lo.FieldSelector.String())
					}
				}
				return cl.List(ctx, list, opts...)
			},
		})
	}
	return b.Build()
}

func nodeClaimEvent(name string, nc *karpenterv1.NodeClaim, eventType, reason, message string, at time.Time) *corev1.Event {
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: nodeClaimEventNamespace},
		InvolvedObject: corev1.ObjectReference{
			APIVersion: "karpenter.sh/v1", Kind: "NodeClaim", Name: nc.Name, UID: nc.UID,
		},
		Type: eventType, Reason: reason, Message: message,
		FirstTimestamp: metav1.NewTime(at),
	}
}

func baseObjects(objs ...client.Object) []client.Object {
	return append([]client.Object{makeNodeClassUnstructured(testConfig.DefaultName)}, objs...)
}

// newTrackedProvisioner returns a provisioner tracking the test NodePool, as ProvisionNodes does.
func newTrackedProvisioner(c client.Client) *KarpenterProvisioner {
	p := NewKarpenterProvisioner(c, testConfig)
	p.provisioningFailures.track(testPoolName, testPoolUID)
	return p
}

func nodeClaimCondition(t *testing.T, p *KarpenterProvisioner, targetNodeCount int32) (nodeClaimCond, resourceCond metav1.Condition) {
	t.Helper()
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", targetNodeCount, nil, nil)
	require.Equal(t, testPoolName, NodePoolName(ws.Namespace, ws.Name))
	conditions, err := p.CollectNodeStatusInfo(context.Background(), ws)
	require.NoError(t, err)
	byType := map[string]metav1.Condition{}
	for _, c := range conditions {
		byType[c.Type] = c
	}
	return byType[string(kaitov1beta1.ConditionTypeNodeClaimStatus)], byType[string(kaitov1beta1.ConditionTypeResourceStatus)]
}

// A SKU quota error is reported over the generic insufficient capacity error of the retry it causes.
func TestProvisioningFailure_QuotaErrorOutranksLaterInsufficientCapacity(t *testing.T) {
	failing := newWorkspaceNodeClaim("nc-1", testPoolUID, quotaLaunchFailure())
	replacement := newWorkspaceNodeClaim("nc-2", testPoolUID, awaitingLaunch())
	ice := nodeClaimEvent("ice", failing, corev1.EventTypeWarning, insufficientCapacityEventReason,
		"NodeClaim nc-1 event: all requested instance types were unavailable during launch", time.Now())
	counter := &eventListCounter{}
	p := newTrackedProvisioner(newFailureTestClient(counter, baseObjects(replacement, ice)...))

	p.ObserveNodeClaim(nil, failing, false)
	p.ObserveNodeClaim(nil, failing, true)
	p.ObserveNodeClaim(nil, replacement, false)

	nodeClaimCond, resourceCond := nodeClaimCondition(t, p, 1)
	assert.Equal(t, metav1.ConditionFalse, nodeClaimCond.Status)
	assert.Equal(t, "CreateInstanceFailed", nodeClaimCond.Reason)
	assert.Equal(t, quotaLaunchFailure().Message, nodeClaimCond.Message)
	assert.Equal(t, nodeClaimCond.Reason, resourceCond.Reason)
	assert.Zero(t, counter.calls, "no Event lookup once the cloud error is known")
}

// An exhausted regional quota is only reported by an InsufficientCapacityError Event.
func TestProvisioningFailure_InsufficientCapacityEvent(t *testing.T) {
	failed := newWorkspaceNodeClaim("nc-1", testPoolUID, awaitingLaunch())
	replacement := newWorkspaceNodeClaim("nc-2", testPoolUID, awaitingLaunch())
	counter := &eventListCounter{}
	p := newTrackedProvisioner(newFailureTestClient(counter, baseObjects(replacement,
		nodeClaimEvent("ice", failed, corev1.EventTypeWarning, insufficientCapacityEventReason, regionalQuotaMessage, time.Now()),
	)...))
	p.ObserveNodeClaim(nil, failed, true)
	p.ObserveNodeClaim(nil, replacement, false)

	nodeClaimCond, _ := nodeClaimCondition(t, p, 1)
	assert.Equal(t, insufficientCapacityEventReason, nodeClaimCond.Reason)
	assert.Equal(t, nodeclaim.FormatProvisioningMessage(regionalQuotaMessage), nodeClaimCond.Message)
	assert.Equal(t, 1, counter.calls)
	assert.Equal(t, []string{"involvedObject.uid=nc-1-uid,type=Warning"}, counter.selectors)

	nodeClaimCondition(t, p, 1)
	assert.Equal(t, 1, counter.calls, "the Events of a NodeClaim are not listed again once a failure was found")
}

// An asynchronous Azure failure after launch is reported by an AsyncProvisioningError Event.
func TestProvisioningFailure_AsynchronousProvisioningError(t *testing.T) {
	failed := newWorkspaceNodeClaim("nc-1", testPoolUID, launchedOK(), awaitingRegistration())
	replacement := newWorkspaceNodeClaim("nc-2", testPoolUID, awaitingLaunch())
	p := newTrackedProvisioner(newFailureTestClient(nil, baseObjects(replacement,
		nodeClaimEvent("async", failed, corev1.EventTypeWarning, asyncProvisioningEventReason, azureAllocationFailedError, time.Now()),
		nodeClaimEvent("normal", failed, corev1.EventTypeNormal, "Launched", "launched", time.Now()),
	)...))
	p.ObserveNodeClaim(nil, failed, false)
	p.ObserveNodeClaim(nil, failed, true)
	p.ObserveNodeClaim(nil, replacement, false)

	nodeClaimCond, _ := nodeClaimCondition(t, p, 1)
	assert.Equal(t, asyncProvisioningEventReason, nodeClaimCond.Reason)
	assert.True(t, strings.HasPrefix(nodeClaimCond.Message, "AllocationFailed: Allocation failed."), nodeClaimCond.Message)
}

// A launched NodeClaim whose node does not register is progress until it is deleted.
func TestProvisioningFailure_RegistrationTimeout(t *testing.T) {
	booting := newWorkspaceNodeClaim("nc-1", testPoolUID, launchedOK(), awaitingRegistration())
	p := newTrackedProvisioner(newFailureTestClient(nil, baseObjects(booting)...))
	p.ObserveNodeClaim(nil, booting, false)

	nodeClaimCond, _ := nodeClaimCondition(t, p, 1)
	assert.Equal(t, nodeclaim.NodeClaimNotReadyReason, nodeClaimCond.Reason)
	assert.Equal(t, "NodeClaim Registered=Unknown (NodeNotFound): Node not registered with cluster", nodeClaimCond.Message)

	replacement := newWorkspaceNodeClaim("nc-2", testPoolUID, awaitingLaunch())
	p.client = newFailureTestClient(nil, baseObjects(replacement)...)
	p.ObserveNodeClaim(nil, booting, true)
	p.ObserveNodeClaim(nil, replacement, false)

	nodeClaimCond, _ = nodeClaimCondition(t, p, 1)
	assert.Equal(t, NodeClaimRegistrationFailedReason, nodeClaimCond.Reason)
	assert.Equal(t, "NodeClaim nc-1 was deleted after it launched but before its node registered", nodeClaimCond.Message)
}

// Karpenter never fails initialization, so a stuck one is progress.
func TestProvisioningFailure_InitializationIsProgress(t *testing.T) {
	initializing := newWorkspaceNodeClaim("nc-1", testPoolUID, launchedOK(), registeredOK(), status.Condition{
		Type: karpenterv1.ConditionTypeInitialized, Status: metav1.ConditionUnknown, Reason: "ResourceNotRegistered",
		Message: `Resource "nvidia.com/gpu" was requested but not registered`})
	p := newTrackedProvisioner(newFailureTestClient(nil, baseObjects(initializing)...))
	p.ObserveNodeClaim(nil, initializing, false)

	nodeClaimCond, _ := nodeClaimCondition(t, p, 1)
	assert.Equal(t, nodeclaim.NodeClaimNotReadyReason, nodeClaimCond.Reason)
	assert.Equal(t, `NodeClaim Initialized=Unknown (ResourceNotRegistered): Resource "nvidia.com/gpu" was requested but not registered`, nodeClaimCond.Message)
}

// A failure on a launched live NodeClaim is reported even when launched NodeClaims cover the target.
func TestProvisioningFailure_LiveFailureOfLaunchedNodeClaim(t *testing.T) {
	failing := newWorkspaceNodeClaim("nc-1", testPoolUID, launchedOK(), status.Condition{
		Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionFalse, Reason: "MultipleNodesFound",
		Message: "Invariant violated, matched multiple nodes"})
	p := newTrackedProvisioner(newFailureTestClient(nil, baseObjects(failing)...))
	p.ObserveNodeClaim(nil, failing, false)

	nodeClaimCond, _ := nodeClaimCondition(t, p, 1)
	assert.Equal(t, "MultipleNodesFound", nodeClaimCond.Reason)
	assert.Equal(t, "Invariant violated, matched multiple nodes", nodeClaimCond.Message)
}

// Once a replacement has launched, failures of deleted NodeClaims are stale.
func TestProvisioningFailure_LaunchedReplacementHidesStaleFailures(t *testing.T) {
	failed := newWorkspaceNodeClaim("nc-1", testPoolUID, quotaLaunchFailure())
	booting := newWorkspaceNodeClaim("nc-2", testPoolUID, launchedOK(), awaitingRegistration())
	p := newTrackedProvisioner(newFailureTestClient(nil, baseObjects(booting)...))
	p.ObserveNodeClaim(nil, failed, true)
	p.ObserveNodeClaim(nil, booting, false)

	nodeClaimCond, _ := nodeClaimCondition(t, p, 1)
	assert.Equal(t, nodeclaim.NodeClaimNotReadyReason, nodeClaimCond.Reason)

	nodeClaimCond, _ = nodeClaimCondition(t, p, 2)
	assert.Equal(t, "CreateInstanceFailed", nodeClaimCond.Reason,
		"a partially provisioned workspace reports why the remaining node failed")
}

func TestProvisioningFailure_UntrackedNodePoolFallsBackToLiveNodeClaims(t *testing.T) {
	failing := newWorkspaceNodeClaim("nc-1", testPoolUID, quotaLaunchFailure())
	p := NewKarpenterProvisioner(newFailureTestClient(nil, baseObjects(failing)...), testConfig)
	p.ObserveNodeClaim(nil, failing, false)

	nodeClaimCond, _ := nodeClaimCondition(t, p, 1)
	assert.Equal(t, "CreateInstanceFailed", nodeClaimCond.Reason)
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

func nodeClassWithoutStatus() *unstructured.Unstructured {
	obj := makeNodeClassUnstructured(testConfig.DefaultName)
	delete(obj.Object, "status")
	return obj
}

func TestProvisioningFailure_NodeClass(t *testing.T) {
	failing := newWorkspaceNodeClaim("nc-1", testPoolUID, quotaLaunchFailure())
	testcases := map[string]struct {
		nodeClass       client.Object
		expectedReason  string
		expectedMessage string
	}{
		"missing": {
			expectedReason:  NodeClassNotReadyReason,
			expectedMessage: `NodeClass "image-family-ubuntu" is not ready (NotFound): NodeClass does not exist`,
		},
		"Ready=False": {
			nodeClass:       makeNodeClass(testConfig.DefaultName, "False", "ImagesNotReady", "failed to resolve images"),
			expectedReason:  NodeClassNotReadyReason,
			expectedMessage: `NodeClass "image-family-ubuntu" is not ready (ImagesNotReady): failed to resolve images`,
		},
		"Ready=Unknown is progress": {
			nodeClass:       makeNodeClass(testConfig.DefaultName, "Unknown", "AwaitingReconciliation", "object is awaiting reconciliation"),
			expectedReason:  nodeclaim.NodeClaimNotReadyReason,
			expectedMessage: `NodeClass "image-family-ubuntu" is not ready (AwaitingReconciliation): object is awaiting reconciliation`,
		},
	}
	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			objs := []client.Object{failing}
			if tc.nodeClass != nil {
				objs = append(objs, tc.nodeClass)
			}
			p := newTrackedProvisioner(newFailureTestClient(nil, objs...))
			p.ObserveNodeClaim(nil, failing, false)

			nodeClaimCond, _ := nodeClaimCondition(t, p, 1)
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

func TestProvisionNodesAndDeleteNodesTrackTheNodePool(t *testing.T) {
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)
	// The fake client does not assign UIDs like the API server does.
	c := newFakeClientWithInterceptors(interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			obj.SetUID(types.UID(obj.GetName() + "-uid"))
			return cl.Create(ctx, obj, opts...)
		},
	}, makeNodeClassUnstructured(testConfig.DefaultName))
	p := NewKarpenterProvisioner(c, testConfig)
	require.NoError(t, p.ProvisionNodes(context.Background(), ws))

	np := &karpenterv1.NodePool{}
	require.NoError(t, p.client.Get(context.Background(), types.NamespacedName{Name: testPoolName}, np))
	failing := newWorkspaceNodeClaim("nc-1", np.UID, quotaLaunchFailure())
	p.ObserveNodeClaim(nil, failing, true)
	_, ok := p.provisioningFailures.rootCause(testPoolName)
	assert.True(t, ok, "ProvisionNodes tracks the NodePool it creates")

	require.NoError(t, p.DeleteNodes(context.Background(), ws))
	p.ObserveNodeClaim(nil, failing, true)
	_, ok = p.provisioningFailures.rootCause(testPoolName)
	assert.False(t, ok, "DeleteNodes stops tracking the NodePool")
}
