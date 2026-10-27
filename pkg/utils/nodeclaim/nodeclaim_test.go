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
	"errors"
	"strings"
	"testing"

	azurev1beta1 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1beta1"
	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/apis"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestCreateNodeClaim(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
	}{
		"NodeClaim creation fails": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("failed to create nodeClaim"))
			},
			expectedError: errors.New("failed to create nodeClaim"),
		},
		"A nodeClaim is successfully created": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			mockNodeClaim := &test.MockNodeClaim
			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
			err := CreateNodeClaim(context.Background(), mockNodeClaim, mockClient)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestWaitForPendingNodeClaims(t *testing.T) {
	testcases := map[string]struct {
		callMocks           func(c *test.MockClient)
		nodeClaimConditions []status.Condition
		expectedError       error
	}{
		"Fail to list nodeClaims because associated nodeClaims cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("failed to retrieve nodeClaims"))
			},
			expectedError: errors.New("failed to retrieve nodeClaims"),
		},
		"Fail to list nodeClaims because nodeClaim status cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				nodeClaimList := test.MockNodeClaimList
				relevantMap := c.CreateMapWithType(nodeClaimList)
				c.CreateOrUpdateObjectInMap(&test.MockNodeClaim)

				//insert nodeClaim objects into the map
				for _, obj := range test.MockNodeClaimList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)

					relevantMap[objKey] = &m
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("fail to get nodeClaim"))
			},
			nodeClaimConditions: []status.Condition{
				{
					Type:   karpenterv1.ConditionTypeInitialized,
					Status: metav1.ConditionFalse,
				},
			},
			expectedError: errors.New("fail to get nodeClaim"),
		},
		"Successfully waits for all pending nodeClaims": {
			callMocks: func(c *test.MockClient) {
				nodeClaimList := test.MockNodeClaimList
				relevantMap := c.CreateMapWithType(nodeClaimList)
				c.CreateOrUpdateObjectInMap(&test.MockNodeClaim)

				//insert nodeClaim objects into the map
				for _, obj := range test.MockNodeClaimList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)

					relevantMap[objKey] = &m
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
			},
			nodeClaimConditions: []status.Condition{
				{
					Type:   string(apis.ConditionReady),
					Status: metav1.ConditionTrue,
				},
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			mockNodeClaim := &karpenterv1.NodeClaim{}

			mockClient.UpdateCb = func(key types.NamespacedName) {
				mockClient.GetObjectFromMap(mockNodeClaim, key)
				mockNodeClaim.Status.Conditions = tc.nodeClaimConditions
				mockClient.CreateOrUpdateObjectInMap(mockNodeClaim)
			}

			err := WaitForPendingNodeClaims(context.Background(), test.MockWorkspaceWithPreset, mockClient)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestGenerateNodeClaimManifest(t *testing.T) {
	testCases := []struct {
		name                     string
		cloudProvider            string
		workspace                *kaitov1beta1.Workspace
		useOptions               bool
		options                  ManifestOptions
		expectedNodeClassKind    string
		expectedNodeClassGroup   string
		expectedImageFamily      string
		expectImageFamilyPresent bool
	}{
		{
			name:                     "azure default manifest",
			cloudProvider:            consts.AzureCloudName,
			workspace:                test.MockWorkspaceWithPreset,
			useOptions:               false,
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectedImageFamily:      "ubuntu",
			expectImageFamilyPresent: true,
		},
		{
			name:                     "azure default node image family is set via startup parameter",
			cloudProvider:            consts.AzureCloudName,
			workspace:                test.MockWorkspaceWithPreset.DeepCopy(),
			useOptions:               true,
			options:                  ManifestOptions{DefaultNodeImageFamily: "AzureLinux"},
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectedImageFamily:      "azurelinux",
			expectImageFamilyPresent: true,
		},
		{
			name:          "workspace annotation overrides startup parameter when annotation is supported",
			cloudProvider: consts.AzureCloudName,
			workspace: func() *kaitov1beta1.Workspace {
				w := test.MockWorkspaceWithPreset.DeepCopy()
				w.Annotations = map[string]string{kaitov1beta1.AnnotationNodeImageFamily: "Ubuntu"}
				return w
			}(),
			useOptions:               true,
			options:                  ManifestOptions{DefaultNodeImageFamily: "AzureLinux"},
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectedImageFamily:      "ubuntu",
			expectImageFamilyPresent: true,
		},
		{
			name:          "unsupported workspace annotation is ignored when startup parameter is supported",
			cloudProvider: consts.AzureCloudName,
			workspace: func() *kaitov1beta1.Workspace {
				w := test.MockWorkspaceWithPreset.DeepCopy()
				w.Annotations = map[string]string{kaitov1beta1.AnnotationNodeImageFamily: "CustomLinux"}
				return w
			}(),
			useOptions:               true,
			options:                  ManifestOptions{DefaultNodeImageFamily: "AzureLinux"},
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectedImageFamily:      "azurelinux",
			expectImageFamilyPresent: true,
		},
		{
			name:          "unsupported startup parameter and unsupported workspace annotation are both ignored",
			cloudProvider: consts.AzureCloudName,
			workspace: func() *kaitov1beta1.Workspace {
				w := test.MockWorkspaceWithPreset.DeepCopy()
				w.Annotations = map[string]string{kaitov1beta1.AnnotationNodeImageFamily: "CustomLinux"}
				return w
			}(),
			useOptions:               true,
			options:                  ManifestOptions{DefaultNodeImageFamily: "CustomLinux"},
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectImageFamilyPresent: false,
		},
		{
			name:                     "image family defaults to ubuntu when startup parameter is empty and workspace annotation missing",
			cloudProvider:            consts.AzureCloudName,
			workspace:                test.MockWorkspaceWithPreset.DeepCopy(),
			useOptions:               true,
			options:                  ManifestOptions{DefaultNodeImageFamily: ""},
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectedImageFamily:      "ubuntu",
			expectImageFamilyPresent: true,
		},
		{
			name:                     "aws default manifest",
			cloudProvider:            consts.AWSCloudName,
			workspace:                test.MockWorkspaceWithPreset,
			useOptions:               false,
			expectedNodeClassKind:    "EC2NodeClass",
			expectedNodeClassGroup:   "karpenter.k8s.aws",
			expectedImageFamily:      "ubuntu",
			expectImageFamilyPresent: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", tc.cloudProvider)

			workspace := tc.workspace.DeepCopy()
			if tc.name == "image family defaults to ubuntu when startup parameter is empty and workspace annotation missing" {
				workspace.Annotations = nil
			}

			var nodeClaim *karpenterv1.NodeClaim
			if tc.useOptions {
				nodeClaim = GenerateNodeClaimManifestWithOptions("0", workspace, tc.options)
			} else {
				nodeClaim = GenerateNodeClaimManifest("0", workspace)
			}

			assert.Check(t, nodeClaim != nil, "NodeClaim must not be nil")
			assert.Equal(t, nodeClaim.Namespace, workspace.Namespace, "NodeClaim must have same namespace as workspace")
			assert.Equal(t, nodeClaim.Labels[kaitov1beta1.LabelWorkspaceName], workspace.Name, "label must have same workspace name as workspace")
			assert.Equal(t, nodeClaim.Labels[kaitov1beta1.LabelWorkspaceNamespace], workspace.Namespace, "label must have same workspace namespace as workspace")
			assert.Equal(t, nodeClaim.Labels[consts.LabelNodePool], consts.KaitoNodePoolName, "label must have same labels as workspace label selector")
			assert.Equal(t, nodeClaim.Annotations[karpenterv1.DoNotDisruptAnnotationKey], "true", "label must have do not disrupt annotation")
			assert.Equal(t, len(nodeClaim.Spec.Requirements), 4, " NodeClaim must have 4 NodeSelector Requirements")
			assert.Equal(t, nodeClaim.Spec.Requirements[1].Values[0], workspace.Resource.InstanceType, "NodeClaim must have same instance type as workspace")
			assert.Equal(t, nodeClaim.Spec.Requirements[2].Key, corev1.LabelOSStable, "NodeClaim must have OS label")
			assert.Check(t, nodeClaim.Spec.NodeClassRef != nil, "NodeClaim must have NodeClassRef")
			assert.Equal(t, nodeClaim.Spec.NodeClassRef.Kind, tc.expectedNodeClassKind, "NodeClaim must have expected nodeclass kind")
			assert.Equal(t, nodeClaim.Spec.NodeClassRef.Group, tc.expectedNodeClassGroup, "NodeClaim must have expected nodeclass group")

			imageFamily, found := nodeClaim.Annotations[kaitov1beta1.AnnotationNodeImageFamily]
			if tc.expectImageFamilyPresent {
				assert.Check(t, found, "NodeClaim should contain node image family annotation")
				assert.Equal(t, imageFamily, tc.expectedImageFamily, "NodeClaim image family annotation mismatch")
			} else {
				assert.Check(t, !found, "NodeClaim should not contain node image family annotation")
			}
		})
	}
}

func TestFirstNodeClaimProvisioningState(t *testing.T) {
	nc := func(conds ...status.Condition) *karpenterv1.NodeClaim {
		return &karpenterv1.NodeClaim{Status: karpenterv1.NodeClaimStatus{Conditions: conds}}
	}
	deleting := func(conds ...status.Condition) *karpenterv1.NodeClaim {
		n := nc(conds...)
		now := metav1.Now()
		n.DeletionTimestamp = &now
		n.Finalizers = []string{"karpenter.sh/termination"}
		return n
	}

	testcases := map[string]struct {
		nodeClaims      []*karpenterv1.NodeClaim
		expectedFound   bool
		expectedReason  string
		expectedMessage string
	}{
		"no node claims": {
			nodeClaims:    nil,
			expectedFound: false,
		},
		"pending launched with no message is skipped": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown}),
			},
			expectedFound: false,
		},
		"initial AwaitingReconciliation state is surfaced as in-progress": {
			// Freshly created NodeClaim: operatorpkg initializes conditions to
			// Unknown/AwaitingReconciliation before launch is attempted. With no
			// real error present, this in-progress state is surfaced.
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(
					status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation", Message: "object is awaiting reconciliation"},
					status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation", Message: "object is awaiting reconciliation"},
					status.Condition{Type: karpenterv1.ConditionTypeInitialized, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation", Message: "object is awaiting reconciliation"},
				),
			},
			expectedFound:   true,
			expectedReason:  "AwaitingReconciliation",
			expectedMessage: "object is awaiting reconciliation",
		},
		"ready node claim has no state": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionTrue}),
			},
			expectedFound: false,
		},
		"launched failure surfaced": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{
					Type:    karpenterv1.ConditionTypeLaunched,
					Status:  metav1.ConditionUnknown,
					Reason:  "SubscriptionQuotaReached",
					Message: "Family Cores quota exceeded",
				}),
			},
			expectedFound:   true,
			expectedReason:  "SubscriptionQuotaReached",
			expectedMessage: "Family Cores quota exceeded",
		},
		"launched takes priority over registered": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(
					status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionUnknown, Reason: "NotRegistered", Message: "node not registered"},
					status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "Unauthorized", Message: "unauthorized to create VM"},
				),
			},
			expectedFound:   true,
			expectedReason:  "Unauthorized",
			expectedMessage: "unauthorized to create VM",
		},
		"real error takes priority over AwaitingReconciliation across node claims": {
			// nc1 is still awaiting reconciliation, nc2 has a real error on the
			// same (Launched) stage. The real error must win regardless of order.
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation", Message: "object is awaiting reconciliation"}),
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "SubscriptionQuotaReached", Message: "Family Cores quota exceeded"}),
			},
			expectedFound:   true,
			expectedReason:  "SubscriptionQuotaReached",
			expectedMessage: "Family Cores quota exceeded",
		},
		"empty message is skipped": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation", Message: ""}),
			},
			expectedFound: false,
		},
		"deleting node claim is skipped": {
			nodeClaims: []*karpenterv1.NodeClaim{
				deleting(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionFalse, Reason: "Gone", Message: "gone"}),
			},
			expectedFound: false,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			reason, message, found := FirstNodeClaimProvisioningState(tc.nodeClaims)
			assert.Equal(t, found, tc.expectedFound)
			assert.Equal(t, reason, tc.expectedReason)
			assert.Equal(t, message, tc.expectedMessage)
		})
	}
}

func TestFirstNodeClaimProvisioningStateTruncatesMessage(t *testing.T) {
	long := strings.Repeat("a", maxProvisioningErrorMessageLen+50)
	nodeClaims := []*karpenterv1.NodeClaim{
		{Status: karpenterv1.NodeClaimStatus{Conditions: []status.Condition{
			{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "LaunchFailed", Message: long},
		}}},
	}
	_, message, found := FirstNodeClaimProvisioningState(nodeClaims)
	assert.Check(t, found)
	assert.Equal(t, len(message), maxProvisioningErrorMessageLen+len("..."))
}

// failureOrProgress composes the NodeClaim helpers like the Karpenter provisioner does.
func failureOrProgress(nodeClaims []*karpenterv1.NodeClaim) (reason, message string, found bool) {
	if reason, message, found = RawNodeClaimProvisioningFailure(nodeClaims); found {
		return ValidConditionReason(reason), FormatProvisioningMessage(message), true
	}
	if message, found = NodeClaimProvisioningProgress(nodeClaims); found {
		return NodeClaimNotReadyReason, message, true
	}
	return "", "", false
}

func TestNodeClaimFailureOrProgress(t *testing.T) {
	nc := func(conds ...status.Condition) *karpenterv1.NodeClaim {
		return &karpenterv1.NodeClaim{Status: karpenterv1.NodeClaimStatus{Conditions: conds}}
	}
	deleting := func(conds ...status.Condition) *karpenterv1.NodeClaim {
		n := nc(conds...)
		now := metav1.Now()
		n.DeletionTimestamp = &now
		n.Finalizers = []string{"karpenter.sh/termination"}
		return n
	}

	testcases := map[string]struct {
		nodeClaims      []*karpenterv1.NodeClaim
		expectedFound   bool
		expectedReason  string
		expectedMessage string
	}{
		"no node claims": {
			nodeClaims:    nil,
			expectedFound: false,
		},
		"pending launched without a reason is progress": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown}),
			},
			expectedFound:   true,
			expectedReason:  NodeClaimNotReadyReason,
			expectedMessage: "NodeClaim Launched=Unknown",
		},
		"initial AwaitingReconciliation state is surfaced as progress": {
			// A fresh NodeClaim is Unknown/AwaitingReconciliation; the earliest pending stage is described.
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(
					status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation", Message: "object is awaiting reconciliation"},
					status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation", Message: "object is awaiting reconciliation"},
					status.Condition{Type: karpenterv1.ConditionTypeInitialized, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation", Message: "object is awaiting reconciliation"},
				),
			},
			expectedFound:   true,
			expectedReason:  NodeClaimNotReadyReason,
			expectedMessage: "NodeClaim Launched=Unknown (AwaitingReconciliation): object is awaiting reconciliation",
		},
		"boot progress after launch is surfaced as progress": {
			// Registration and initialization stay Unknown while the node boots, which is progress.
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(
					status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionTrue, Reason: "Launched"},
					status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionUnknown, Reason: "RegistrationHookPending", Message: "hook"},
				),
			},
			expectedFound:   true,
			expectedReason:  NodeClaimNotReadyReason,
			expectedMessage: "NodeClaim Registered=Unknown (RegistrationHookPending): hook",
		},
		"a False lifecycle condition is a failure": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(
					status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionTrue, Reason: "Launched"},
					status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionFalse, Reason: "MultipleNodesFound", Message: "Invariant violated, matched multiple nodes"},
				),
			},
			expectedFound:   true,
			expectedReason:  "MultipleNodesFound",
			expectedMessage: "Invariant violated, matched multiple nodes",
		},
		"launch failure without a message": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "LaunchFailed"}),
			},
			expectedFound:   true,
			expectedReason:  "LaunchFailed",
			expectedMessage: "NodeClaim Launched=Unknown (LaunchFailed)",
		},
		"failure on a later node claim wins over progress on an earlier one": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(
					status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionTrue, Reason: "Launched"},
					status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionUnknown, Reason: "NodeNotFound", Message: "Node not registered with cluster"},
				),
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "CreateInstanceFailed", Message: "quota"}),
			},
			expectedFound:   true,
			expectedReason:  "CreateInstanceFailed",
			expectedMessage: "quota",
		},
		"ready node claim has no state": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionTrue}),
			},
			expectedFound: false,
		},
		"launched failure surfaced": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{
					Type:    karpenterv1.ConditionTypeLaunched,
					Status:  metav1.ConditionUnknown,
					Reason:  "CreateInstanceFailed",
					Message: "Family Cores quota exceeded",
				}),
			},
			expectedFound:   true,
			expectedReason:  "CreateInstanceFailed",
			expectedMessage: "Family Cores quota exceeded",
		},
		"launched takes priority over registered": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(
					status.Condition{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionUnknown, Reason: "NotRegistered", Message: "node not registered"},
					status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "Unauthorized", Message: "unauthorized to create VM"},
				),
			},
			expectedFound:   true,
			expectedReason:  "Unauthorized",
			expectedMessage: "unauthorized to create VM",
		},
		"real error takes priority over AwaitingReconciliation across node claims": {
			// nc1 is still awaiting reconciliation, nc2 has a real error on the
			// same (Launched) stage. The real error must win regardless of order.
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation", Message: "object is awaiting reconciliation"}),
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "CreateInstanceFailed", Message: "Family Cores quota exceeded"}),
			},
			expectedFound:   true,
			expectedReason:  "CreateInstanceFailed",
			expectedMessage: "Family Cores quota exceeded",
		},
		"an invalid failure reason is replaced": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "bad reason!", Message: "boom"}),
			},
			expectedFound:   true,
			expectedReason:  ProvisioningFailedReason,
			expectedMessage: "boom",
		},
		"waiting for the NodeClass to be reconciled is progress": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: nodeClassReadinessUnknownReason, Message: "NodeClass is in Ready=Unknown"}),
			},
			expectedFound:   true,
			expectedReason:  NodeClaimNotReadyReason,
			expectedMessage: "NodeClaim Launched=Unknown (NodeClassReadinessUnknown): NodeClass is in Ready=Unknown",
		},
		"awaiting reconciliation without a message is progress": {
			nodeClaims: []*karpenterv1.NodeClaim{
				nc(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation", Message: ""}),
			},
			expectedFound:   true,
			expectedReason:  NodeClaimNotReadyReason,
			expectedMessage: "NodeClaim Launched=Unknown (AwaitingReconciliation)",
		},
		"failure of a deleting node claim when no live one fails": {
			nodeClaims: []*karpenterv1.NodeClaim{
				deleting(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionFalse, Reason: "Gone", Message: "gone"}),
			},
			expectedFound:   true,
			expectedReason:  "Gone",
			expectedMessage: "gone",
		},
		"progress of a deleting node claim is skipped": {
			nodeClaims: []*karpenterv1.NodeClaim{
				deleting(status.Condition{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "AwaitingReconciliation"}),
			},
			expectedFound: false,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			reason, message, found := failureOrProgress(tc.nodeClaims)
			assert.Equal(t, found, tc.expectedFound)
			assert.Equal(t, reason, tc.expectedReason)
			assert.Equal(t, message, tc.expectedMessage)
		})
	}
}

// azureSubnetFullError mimics an Azure SDK error dump: request line, separator, error code and body.
const azureSubnetFullError = `PUT https://management.azure.com/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/MC_rg_cluster_eastus/providers/Microsoft.Network/networkInterfaces/aks-default-ws1-abcde
--------------------------------------------------------------------------------
RESPONSE 400: 400 Bad Request
ERROR CODE: SubnetIsFull
--------------------------------------------------------------------------------
{
  "error": {
    "code": "SubnetIsFull",
    "message": "Subnet aks-subnet with address prefix 10.224.0.0/16 does not have enough capacity for 1 IP addresses.",
    "details": [{"code": "Inner", "message": "inner detail"}]
  }
}
--------------------------------------------------------------------------------
`

func TestCompactProvisioningMessage(t *testing.T) {
	cases := map[string]struct {
		in, expected string
	}{
		"plain message unchanged": {
			in:       "subscription level on-demand vCPU quota for Standard_NC24ads_A100_v4 has been reached",
			expected: "subscription level on-demand vCPU quota for Standard_NC24ads_A100_v4 has been reached",
		},
		"azure error dump": {
			in: azureSubnetFullError,
			expected: "SubnetIsFull: Subnet aks-subnet with address prefix 10.224.0.0/16 does not have enough capacity for 1 IP addresses. " +
				"(PUT https://management.azure.com/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/MC_rg_cluster_eastus/providers/Microsoft.Network/networkInterfaces/aks-default-ws1-abcde)",
		},
		"body cut off by provider truncation": {
			in:       "virtualMachine.BeginCreateOrUpdate for VM \"aks-x\" failed: PUT https://example\n----\nRESPONSE 409: 409 Conflict\nERROR CODE: OperationNotAllowed\n----\n{\n  \"error\": {\n    \"message\": \"Operation could not be compl...",
			expected: "OperationNotAllowed (virtualMachine.BeginCreateOrUpdate for VM \"aks-x\" failed: PUT https://example)",
		},
		"escaped characters in message": {
			in:       "PUT https://example\nERROR CODE: InvalidParameter\n{\"error\": {\"message\": \"Parameter \\\"osDisk\\\" is invalid\"}}",
			expected: "InvalidParameter: Parameter \"osDisk\" is invalid (PUT https://example)",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, compactProvisioningMessage(tc.in), tc.expected)
		})
	}
}

func TestFormatProvisioningMessageKeepsErrorCode(t *testing.T) {
	// Plain truncation of the raw dump would keep only the request URL.
	assert.Check(t, !strings.Contains(utils.TruncateMessage(azureSubnetFullError, maxProvisioningErrorMessageLen), "SubnetIsFull"))

	got := FormatProvisioningMessage(azureSubnetFullError)
	assert.Check(t, strings.HasPrefix(got, "SubnetIsFull: Subnet aks-subnet"))
	assert.Check(t, len(got) <= maxProvisioningErrorMessageLen+len("..."))
}

func TestCloudErrorCodes(t *testing.T) {
	codes := CloudErrorCodes(azureSubnetFullError)
	assert.Check(t, codes.Has("SubnetIsFull"))
	assert.Check(t, codes.Has("Inner"), "JSON error details are matched")
	assert.Check(t, CloudErrorCodes("failed to create AKS machine \"m\", unhandled provisioning error: code=AllocationFailed, message=x").Has("AllocationFailed"))
	assert.Equal(t, CloudErrorCodes("subscription level on-demand vCPU quota has been reached").Len(), 0)
}

func TestRawNodeClaimProvisioningFailure(t *testing.T) {
	now := metav1.Now()
	deleting := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now, Finalizers: []string{"karpenter.sh/termination"}},
		Status: karpenterv1.NodeClaimStatus{Conditions: []status.Condition{
			{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "CreateInstanceFailed", Message: azureSubnetFullError},
		}},
	}
	live := &karpenterv1.NodeClaim{Status: karpenterv1.NodeClaimStatus{Conditions: []status.Condition{
		{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: "LaunchFailed", Message: "throttled"},
	}}}
	progressing := &karpenterv1.NodeClaim{Status: karpenterv1.NodeClaimStatus{Conditions: []status.Condition{
		{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionUnknown, Reason: awaitingReconciliationReason},
	}}}

	reason, message, found := RawNodeClaimProvisioningFailure([]*karpenterv1.NodeClaim{deleting, progressing})
	assert.Check(t, found)
	assert.Equal(t, reason, "CreateInstanceFailed")
	assert.Equal(t, message, azureSubnetFullError, "the message is not formatted")

	reason, _, found = RawNodeClaimProvisioningFailure([]*karpenterv1.NodeClaim{deleting, live})
	assert.Check(t, found)
	assert.Equal(t, reason, "LaunchFailed", "a live NodeClaim's failure is preferred")

	_, _, found = RawNodeClaimProvisioningFailure([]*karpenterv1.NodeClaim{progressing, nil})
	assert.Check(t, !found)
}

func TestFormatClassifiedProvisioningMessage(t *testing.T) {
	got := FormatClassifiedProvisioningMessage("CreateInstanceFailed", azureSubnetFullError)
	assert.Check(t, strings.HasPrefix(got, "CreateInstanceFailed: SubnetIsFull: Subnet aks-subnet"))
	assert.Check(t, len(got) <= maxProvisioningErrorMessageLen+len("..."))
}

func TestParseClassifiedProvisioningMessage(t *testing.T) {
	reason, message := ParseClassifiedProvisioningMessage(FormatClassifiedProvisioningMessage("InsufficientCapacityError", "no instance types available"))
	assert.Equal(t, "InsufficientCapacityError", reason)
	assert.Equal(t, "no instance types available", message)

	reason, message = ParseClassifiedProvisioningMessage("no reason in front")
	assert.Equal(t, "", reason)
	assert.Equal(t, "no reason in front", message)
}

func TestIsConditionTrue(t *testing.T) {
	nc := &karpenterv1.NodeClaim{Status: karpenterv1.NodeClaimStatus{Conditions: []status.Condition{
		{Type: karpenterv1.ConditionTypeLaunched, Status: metav1.ConditionTrue},
		{Type: karpenterv1.ConditionTypeRegistered, Status: metav1.ConditionUnknown},
	}}}
	assert.Check(t, IsConditionTrue(nc, karpenterv1.ConditionTypeLaunched))
	assert.Check(t, !IsConditionTrue(nc, karpenterv1.ConditionTypeRegistered))
	assert.Check(t, !IsConditionTrue(nc, karpenterv1.ConditionTypeInitialized))
}

// TestLifecycleClassificationMatchesUpstream pins the lifecycle states of the Karpenter modules in go.mod; re-check them on bumps.
func TestLifecycleClassificationMatchesUpstream(t *testing.T) {
	const (
		launched    = karpenterv1.ConditionTypeLaunched
		registered  = karpenterv1.ConditionTypeRegistered
		initialized = karpenterv1.ConditionTypeInitialized
		unknown     = metav1.ConditionUnknown
		falseStatus = metav1.ConditionFalse
	)
	testcases := []struct {
		source        string
		conditionType string
		status        metav1.ConditionStatus
		reason        string
		failure       bool
	}{
		// operatorpkg status.ConditionSet initializes every condition.
		{"operatorpkg status/condition_set.go", launched, unknown, "AwaitingReconciliation", false},
		{"operatorpkg status/condition_set.go", registered, unknown, "AwaitingReconciliation", false},
		{"operatorpkg status/condition_set.go", initialized, unknown, "AwaitingReconciliation", false},
		// karpenter pkg/controllers/nodeclaim/lifecycle/launch.go: CreateError reason or LaunchFailed.
		{"karpenter lifecycle/launch.go", launched, unknown, "LaunchFailed", true},
		// karpenter pkg/controllers/nodeclaim/lifecycle/registration.go.
		{"karpenter lifecycle/registration.go", registered, unknown, "NodeNotFound", false},
		{"karpenter lifecycle/registration.go", registered, falseStatus, "MultipleNodesFound", true},
		// karpenter pkg/controllers/nodeclaim/lifecycle/initialization.go never sets False.
		{"karpenter lifecycle/initialization.go", initialized, unknown, "NodeNotFound", false},
		{"karpenter lifecycle/initialization.go", initialized, unknown, "NodeNotReady", false},
		{"karpenter lifecycle/initialization.go", initialized, unknown, "StartupTaintsExist", false},
		{"karpenter lifecycle/initialization.go", initialized, unknown, "KnownEphemeralTaintsExist", false},
		{"karpenter lifecycle/initialization.go", initialized, unknown, "ResourceNotRegistered", false},
		// karpenter-provider-azure pkg/cloudprovider/cloudprovider.go CreateError reasons.
		{"azure cloudprovider.go", launched, unknown, nodeClassReadinessUnknownReason, false},
		{"azure cloudprovider.go", launched, unknown, "InstanceTypeResolutionFailed", true},
		{"azure cloudprovider.go", launched, unknown, "CreateInstanceFailed", true},
		// karpenter-provider-aws CreateError reasons, mapped from EC2 errors by pkg/errors/errors.go.
		{"aws cloudprovider.go", launched, unknown, nodeClassReadinessUnknownReason, false},
		{"aws cloudprovider.go", launched, unknown, "InstanceTypeResolutionFailed", true},
		{"aws instance.go", launched, unknown, "InstanceTypeFilteringFailed", true},
		{"aws instance.go", launched, unknown, "SubnetResolutionFailed", true},
		{"aws errors.go", launched, unknown, "VCPULimitExceeded", true},
		{"aws errors.go", launched, unknown, "Unauthorized", true},
		{"aws errors.go", launched, unknown, "RequestLimitExceeded", true},
	}
	for _, tc := range testcases {
		t.Run(tc.source+"/"+tc.conditionType+"="+string(tc.status)+"/"+tc.reason, func(t *testing.T) {
			c := status.Condition{Type: tc.conditionType, Status: tc.status, Reason: tc.reason}
			assert.Equal(t, isLifecycleFailure(&c), tc.failure)
		})
	}
}
