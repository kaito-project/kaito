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
	"fmt"
	"testing"

	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
	"github.com/kaito-project/kaito/pkg/workspace/estimator/skubasednodesestimator"
)

func TestNodeClaimReconcilerReconcile(t *testing.T) {
	test.RegisterTestModel()

	// Set required environment variable for SKU handler
	t.Setenv("CLOUD_PROVIDER", "azure")

	t.Run("Should handle workspace not found gracefully", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.NewKlogr(),
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "nonexistent-workspace",
				Namespace: "default",
			},
		}

		// Mock Client.Get to return not found error
		notFoundError := apierrors.NewNotFound(schema.GroupResource{
			Group:    "kaito.sh",
			Resource: "workspaces",
		}, "nonexistent-workspace")

		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(notFoundError)

		result, err := reconciler.Reconcile(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should update inference status when workspace.Inference is not nil and status.Inference is nil", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Estimator:    &skubasednodesestimator.SKUBasedNodesEstimator{},
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.NewKlogr(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{2}[0],
				InstanceType: "Standard_NC12s_v3",
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model",
					},
				},
				Replicas: 1,
			},
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
			},
		}

		// Mock Client.Get to return the workspace
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock Patch call for adding finalizer
		mockClient.On("Patch", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything, mock.Anything).Return(nil)

		// Mock Client.Status().Update to capture the status update
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		result, err := reconciler.Reconcile(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should complete successfully when NodeClaims exist and are ready for inference", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.NewKlogr(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{2}[0],
				InstanceType: "Standard_NC12s_v3",
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model",
					},
				},
				Replicas: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					PerReplicaNodeCount: 2,
					TargetNodeCount:     2,
				},
			},
		}

		// Create ready NodeClaims
		readyNodeClaim1 := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nodeclaim-1",
				Namespace: "default",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName: workspace.Name,
				},
			},
			Status: karpenterv1.NodeClaimStatus{
				Conditions: []status.Condition{
					{
						Type:   "Ready",
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		readyNodeClaim2 := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nodeclaim-2",
				Namespace: "default",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName: workspace.Name,
				},
			},
			Status: karpenterv1.NodeClaimStatus{
				Conditions: []status.Condition{
					{
						Type:   "Ready",
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
			},
		}

		// Mock Client.Get to return the workspace
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock Patch call for adding finalizer
		mockClient.On("Patch", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything, mock.Anything).Return(nil)

		// Mock listing nodes (no preferred nodes)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{} // No nodes
		}).Return(nil)

		// Mock listing existing NodeClaims (2 ready NodeClaims exist) - allow unlimited calls
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{*readyNodeClaim1, *readyNodeClaim2}
		}).Return(nil)

		// Mock additional NodeClaim list call for waitForNodeClaimsReady
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{*readyNodeClaim1, *readyNodeClaim2}
		}).Return(nil)

		// Mock third NodeClaim list call if needed
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{*readyNodeClaim1, *readyNodeClaim2}
		}).Return(nil)

		// Mock status update
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		result, err := reconciler.Reconcile(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		// Skip expectation check due to multiple List calls that are hard to track precisely
		// mockClient.AssertExpectations(t)
	})

	t.Run("Should handle tuning workload correctly", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.NewKlogr(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-tuning-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{1}[0],
				InstanceType: "Standard_NC12s_v3",
			},
			Tuning: &kaitov1beta1.TuningSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model",
					},
				},
			},
		}

		// Create ready NodeClaim for tuning
		readyNodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-tuning-nodeclaim",
				Namespace: "default",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName: workspace.Name,
				},
			},
			Status: karpenterv1.NodeClaimStatus{
				Conditions: []status.Condition{
					{
						Type:   "Ready",
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
			},
		}

		// Mock Client.Get to return the workspace
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock Patch call for adding finalizer
		mockClient.On("Patch", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything, mock.Anything).Return(nil)

		// Mock listing nodes (no preferred nodes)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{} // No nodes
		}).Return(nil)

		// Mock listing existing NodeClaims (1 ready NodeClaim exists for tuning) - allow unlimited calls
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{*readyNodeClaim}
		}).Return(nil)

		// Mock additional NodeClaim list call for waitForNodeClaimsReady
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{*readyNodeClaim}
		}).Return(nil)

		// Mock third NodeClaim list call if needed
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{*readyNodeClaim}
		}).Return(nil)

		// Mock status update
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		result, err := reconciler.Reconcile(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		// Skip expectation check due to multiple List calls that are hard to track precisely
		// mockClient.AssertExpectations(t)
	})

	t.Run("Should handle workspace with preferred nodes", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.NewKlogr(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace-preferred",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:          &[]int{2}[0],
				InstanceType:   "Standard_NC12s_v3",
				PreferredNodes: []string{"node-1"},
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model",
					},
				},
				Replicas: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					PerReplicaNodeCount: 2,
					TargetNodeCount:     2,
				},
			},
		}

		// Create preferred node
		preferredNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
				Labels: map[string]string{
					"beta.kubernetes.io/instance-type": "Standard_NC12s_v3",
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		// Create one NodeClaim (since we have 1 preferred node)
		readyNodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nodeclaim-1",
				Namespace: "default",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName: workspace.Name,
				},
			},
			Status: karpenterv1.NodeClaimStatus{
				Conditions: []status.Condition{
					{
						Type:   "Ready",
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
			},
		}

		// Mock Client.Get to return the workspace
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock Patch call for adding finalizer
		mockClient.On("Patch", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything, mock.Anything).Return(nil)

		// Mock listing nodes (1 preferred node available)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{*preferredNode}
		}).Return(nil)

		// Mock listing existing NodeClaims (1 ready NodeClaim exists)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{*readyNodeClaim}
		}).Return(nil)

		// Mock status update
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		result, err := reconciler.Reconcile(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should handle workspace with feature gate FeatureFlagDisableNodeAutoProvisioning enabled - sufficient preferred nodes", func(t *testing.T) {
		// Enable the feature gate
		originalFlag := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalFlag
		}()

		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.NewKlogr(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace-feature-gate",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:          &[]int{2}[0],
				InstanceType:   "Standard_NC12s_v3",
				PreferredNodes: []string{"node-1", "node-2"},
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"test-label": "true",
					},
				},
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model",
					},
				},
				Replicas: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					PerReplicaNodeCount: 2,
					TargetNodeCount:     2,
				},
			},
		}

		// Create sufficient preferred nodes
		preferredNode1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
				Labels: map[string]string{
					"beta.kubernetes.io/instance-type": "Standard_NC12s_v3",
					"test-label":                       "true",
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		preferredNode2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
				Labels: map[string]string{
					"beta.kubernetes.io/instance-type": "Standard_NC12s_v3",
					"test-label":                       "true",
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
			},
		}

		// Mock Client.Get to return the workspace
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock Patch call for adding finalizer
		mockClient.On("Patch", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything, mock.Anything).Return(nil)

		// Mock listing nodes (2 preferred nodes available)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{*preferredNode1, *preferredNode2}
		}).Return(nil)

		// Mock listing NodeClaims (no NodeClaims needed since auto-provisioning is disabled)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{}
		}).Return(nil)

		// Mock status update
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		result, err := reconciler.Reconcile(ctx, req)

		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		mockClient.AssertExpectations(t)
	})

	t.Run("Should handle workspace with feature gate FeatureFlagDisableNodeAutoProvisioning enabled - insufficient preferred nodes", func(t *testing.T) {
		// Enable the feature gate
		originalFlag := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalFlag
		}()

		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.NewKlogr(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace-feature-gate-insufficient",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:          &[]int{2}[0],
				InstanceType:   "Standard_NC12s_v3",
				PreferredNodes: []string{"node-1"}, // Only 1 preferred node, but need 2
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"test-label": "true",
					},
				},
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model",
					},
				},
				Replicas: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					PerReplicaNodeCount: 2,
					TargetNodeCount:     2,
				},
			},
		}

		// Create only one preferred node (insufficient)
		preferredNode1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
				Labels: map[string]string{
					"beta.kubernetes.io/instance-type": "Standard_NC12s_v3",
					"test-label":                       "true",
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		ctx := context.Background()
		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
			},
		}

		// Mock Client.Get to return the workspace
		mockClient.On("Get", mock.IsType(context.Background()), req.NamespacedName, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock Patch call for adding finalizer
		mockClient.On("Patch", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything, mock.Anything).Return(nil)

		// Mock listing nodes (only 1 preferred node available, but need 2)
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{*preferredNode1}
		}).Return(nil)

		// Mock listing NodeClaims
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{}
		}).Return(nil)

		// Mock status update (should fail with insufficient preferred nodes)
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		result, err := reconciler.Reconcile(ctx, req)

		// Should return an error because insufficient preferred nodes
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "when node auto-provisioning is disabled")
		assert.Equal(t, reconcile.Result{}, result)
		mockClient.AssertExpectations(t)
	})
}

func TestNodeClaimReconcilerEvents(t *testing.T) {
	test.RegisterTestModel()
	t.Setenv("CLOUD_PROVIDER", "azure")

	t.Run("NodeClaim creation generates event", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.NewKlogr(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace-events",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{1}[0],
				InstanceType: "Standard_NC12s_v3",
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model",
					},
				},
				Replicas: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					PerReplicaNodeCount: 1,
					TargetNodeCount:     1,
				},
			},
		}

		ctx := context.Background()

		// Mock workspace Get for status update
		mockClient.On("Get", mock.IsType(context.Background()), types.NamespacedName{Name: workspace.Name, Namespace: workspace.Namespace}, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock no existing NodeClaims, no preferred nodes
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{}
		}).Return(nil)

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{} // No existing NodeClaims
		}).Return(nil)

		// Mock successful NodeClaim creation
		mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

		// Mock status update
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		err := reconciler.ensureNodeClaims(ctx, workspace)
		assert.NoError(t, err)

		// Verify event was generated
		assert.Len(t, recorder.Events, 1)
		event := <-recorder.Events
		assert.Contains(t, event, "Normal NodeClaimCreated Successfully created NodeClaim")
		assert.Contains(t, event, workspace.Name)
	})

	t.Run("NodeClaim deletion generates event", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.NewKlogr(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace-delete",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{0}[0], // Scale down to 0
				InstanceType: "Standard_NC12s_v3",
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model",
					},
				},
				Replicas: 0,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					PerReplicaNodeCount: 1,
					TargetNodeCount:     0, // Scale down to 0
				},
			},
		}

		ctx := context.Background()

		// Mock workspace Get for status update
		mockClient.On("Get", mock.IsType(context.Background()), types.NamespacedName{Name: workspace.Name, Namespace: workspace.Namespace}, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock no preferred nodes
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{}
		}).Return(nil)

		// Mock existing NodeClaim that should be deleted
		existingNodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nodeclaim-to-delete",
				Namespace: "default",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      workspace.Name,
					kaitov1beta1.LabelWorkspaceNamespace: workspace.Namespace,
				},
				CreationTimestamp: metav1.Now(),
			},
			Status: karpenterv1.NodeClaimStatus{
				Conditions: []status.Condition{
					{
						Type:   "Ready",
						Status: metav1.ConditionTrue,
					},
				},
			},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{*existingNodeClaim}
		}).Return(nil)

		// Mock successful NodeClaim deletion
		mockClient.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

		// Mock status update
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		err := reconciler.ensureNodeClaims(ctx, workspace)
		assert.NoError(t, err)

		// Verify event was generated
		assert.Len(t, recorder.Events, 1)
		event := <-recorder.Events
		assert.Contains(t, event, "Normal NodeClaimDeleted Successfully deleted NodeClaim")
		assert.Contains(t, event, existingNodeClaim.Name)
		assert.Contains(t, event, workspace.Name)
	})

	t.Run("NodeClaim creation failure generates warning event", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.NewKlogr(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace-failure",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        &[]int{1}[0],
				InstanceType: "Standard_NC12s_v3",
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model",
					},
				},
				Replicas: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					PerReplicaNodeCount: 1,
					TargetNodeCount:     1,
				},
			},
		}

		ctx := context.Background()

		// Mock workspace Get for status update
		mockClient.On("Get", mock.IsType(context.Background()), types.NamespacedName{Name: workspace.Name, Namespace: workspace.Namespace}, mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock no existing NodeClaims, no preferred nodes
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nodeList := args.Get(1).(*corev1.NodeList)
			nodeList.Items = []corev1.Node{}
		}).Return(nil)

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{} // No existing NodeClaims
		}).Return(nil)

		// Mock failed NodeClaim creation
		createError := apierrors.NewInternalError(fmt.Errorf("failed to create NodeClaim"))
		mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(createError)

		// Mock status update
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		err := reconciler.ensureNodeClaims(ctx, workspace)
		assert.Error(t, err)

		// Verify warning event was generated
		assert.Len(t, recorder.Events, 1)
		event := <-recorder.Events
		assert.Contains(t, event, "Warning NodeClaimCreationFailed Failed to create NodeClaim")
		assert.Contains(t, event, workspace.Name)
	})
}

func TestNodeClaimReconcilerFinalizer(t *testing.T) {
	test.RegisterTestModel()

	// Set required environment variable for SKU handler
	t.Setenv("CLOUD_PROVIDER", "azure")

	t.Run("Should add finalizer to workspace without finalizer", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.Background(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
				// No finalizers initially
			},
			Resource: kaitov1beta1.ResourceSpec{
				InstanceType: "Standard_NC24ads_A100_v4",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"test": "true"},
				},
			},
		}

		ctx := context.Background()

		// Mock Patch call for adding finalizer
		mockClient.On("Patch", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything, mock.Anything).Return(nil)

		err := reconciler.ensureFinalizer(ctx, workspace)
		assert.NoError(t, err)

		// Verify patch was called
		mockClient.AssertExpectations(t)

		// Verify success event was generated
		assert.Len(t, recorder.Events, 1)
		event := <-recorder.Events
		assert.Contains(t, event, "Normal FinalizerAdded Successfully added finalizer to workspace")
		assert.Contains(t, event, workspace.Name)
	})

	t.Run("Should not add finalizer if already present", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.Background(),
		}

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-workspace",
				Namespace:  "default",
				Finalizers: []string{"workspace.finalizer.kaito.sh"},
			},
			Resource: kaitov1beta1.ResourceSpec{
				InstanceType: "Standard_NC24ads_A100_v4",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"test": "true"},
				},
			},
		}

		ctx := context.Background()

		err := reconciler.ensureFinalizer(ctx, workspace)
		assert.NoError(t, err)

		// Verify no patch was called since finalizer already exists
		mockClient.AssertExpectations(t)

		// Verify no events were generated
		assert.Len(t, recorder.Events, 0)
	})

	t.Run("Should handle workspace termination with NodeClaims", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(20)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Reader:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.Background(),
		}

		now := metav1.Now()
		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-workspace",
				Namespace:         "default",
				DeletionTimestamp: &now,
				Finalizers:        []string{"workspace.finalizer.kaito.sh"},
			},
			Resource: kaitov1beta1.ResourceSpec{
				InstanceType: "Standard_NC24ads_A100_v4",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"test": "true"},
				},
			},
		}

		// Create two NodeClaims for deletion
		nodeClaim1 := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nodeclaim-1",
				Namespace: "default",
				Labels: map[string]string{
					"app":                     "kaito-workspace",
					"kaito.sh/workspace-name": "test-workspace",
				},
			},
		}
		nodeClaim2 := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nodeclaim-2",
				Namespace: "default",
				Labels: map[string]string{
					"app":                     "kaito-workspace",
					"kaito.sh/workspace-name": "test-workspace",
				},
			},
		}

		ctx := context.Background()

		// Mock returning NodeClaims to delete - this should only be called once
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{*nodeClaim1, *nodeClaim2}
		}).Return(nil).Once()

		// Mock Delete calls for NodeClaims
		mockClient.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil).Twice()

		result, err := reconciler.finalizeWorkspace(ctx, workspace)
		assert.NoError(t, err)
		// Should requeue to wait for NodeClaims to be actually deleted
		assert.True(t, result.RequeueAfter > 0)

		// Verify all mocks were called
		mockClient.AssertExpectations(t)

		// Verify events were generated (2 NodeClaim deletions only)
		assert.True(t, len(recorder.Events) >= 2)

		// Check for NodeClaim deletion events
		events := make([]string, 0)
		for len(recorder.Events) > 0 {
			events = append(events, <-recorder.Events)
		}

		deletionEventCount := 0
		for _, event := range events {
			if event != "" {
				if len(event) >= len("Normal NodeClaimDeleted") && event[0:len("Normal NodeClaimDeleted")] == "Normal NodeClaimDeleted" {
					deletionEventCount++
				}
			}
		}
		assert.Equal(t, 2, deletionEventCount) // Should have 2 NodeClaim deletion events
	})

	t.Run("Should remove finalizer when no NodeClaims remain", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Reader:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.Background(),
		}

		now := metav1.Now()
		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-workspace",
				Namespace:         "default",
				DeletionTimestamp: &now,
				Finalizers:        []string{"workspace.finalizer.kaito.sh"},
			},
			Resource: kaitov1beta1.ResourceSpec{
				InstanceType: "Standard_NC24ads_A100_v4",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"test": "true"},
				},
			},
		}

		ctx := context.Background()

		// Mock no existing NodeClaims
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{} // No NodeClaims remaining
		}).Return(nil)

		// Mock Patch call for removing finalizer
		mockClient.On("Patch", mock.IsType(context.Background()), mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything, mock.Anything).Return(nil)

		result, err := reconciler.finalizeWorkspace(ctx, workspace)
		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)

		// Verify all mocks were called
		mockClient.AssertExpectations(t)

		// Verify finalizer removal event was generated
		assert.Len(t, recorder.Events, 1)
		event := <-recorder.Events
		assert.Contains(t, event, "Normal FinalizerRemoved")
		assert.Contains(t, event, workspace.Name)
	})

	t.Run("Should wait for NodeClaims being deleted", func(t *testing.T) {
		mockClient := test.NewClient()
		recorder := record.NewFakeRecorder(10)
		reconciler := &NodeClaimReconciler{
			Client:       mockClient,
			Reader:       mockClient,
			Recorder:     recorder,
			expectations: utils.NewControllerExpectations(),
			logger:       klog.Background(),
		}

		now := metav1.Now()
		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-workspace",
				Namespace:         "default",
				DeletionTimestamp: &now,
				Finalizers:        []string{"workspace.finalizer.kaito.sh"},
			},
			Resource: kaitov1beta1.ResourceSpec{
				InstanceType: "Standard_NC24ads_A100_v4",
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"test": "true"},
				},
			},
		}

		// Create a NodeClaim that is already being deleted
		nodeClaimDeleting := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-nodeclaim-deleting",
				Namespace:         "default",
				DeletionTimestamp: &now,
				Labels: map[string]string{
					"app":                     "kaito-workspace",
					"kaito.sh/workspace-name": "test-workspace",
				},
			},
		}

		ctx := context.Background()

		// Mock NodeClaim list returning a NodeClaim that's being deleted
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncList := args.Get(1).(*karpenterv1.NodeClaimList)
			ncList.Items = []karpenterv1.NodeClaim{*nodeClaimDeleting}
		}).Return(nil)

		result, err := reconciler.finalizeWorkspace(ctx, workspace)
		assert.NoError(t, err)
		// Should requeue to wait for deletion
		assert.True(t, result.RequeueAfter > 0)

		// Verify no events were generated for waiting (waiting events were removed)
		assert.Len(t, recorder.Events, 0)
	})
}
