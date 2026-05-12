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
	"testing"

	"github.com/awslabs/operatorpkg/status"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

var testConfig = NodeClassConfig{
	Group:        "karpenter.azure.com",
	Kind:         "AKSNodeClass",
	Version:      "v1beta1",
	ResourceName: "aksnodeclasses",
	DefaultName:  "image-family-ubuntu",
}

// testScheme returns a scheme with all types needed for fake.Client tests.
func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = apiextensionsv1.AddToScheme(s)
	_ = utils.KarpenterSchemeBuilder.AddToScheme(s)
	return s
}

// newFakeClient creates a fake.Client with the test scheme and the given objects.
func newFakeClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(objs...).Build()
}

// newFakeClientWithInterceptors creates a fake.Client with custom interceptor functions for error injection.
func newFakeClientWithInterceptors(funcs interceptor.Funcs, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(objs...).WithInterceptorFuncs(funcs).Build()
}

// makeReadyNode creates a ready Node with the given name, instance type, and extra labels.
// Always includes the workspace label selector label ("apps": "llm").
func makeReadyNode(name, instanceType string, extraLabels map[string]string) *corev1.Node {
	labels := map[string]string{
		"apps":                         "llm",
		corev1.LabelInstanceTypeStable: instanceType,
	}
	for k, v := range extraLabels {
		labels[k] = v
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

// makeNodeClassUnstructured creates an unstructured NodeClass object with Ready=True.
func makeNodeClassUnstructured(name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   testConfig.Group,
		Version: testConfig.Version,
		Kind:    testConfig.Kind,
	})
	obj.SetName(name)
	obj.Object["status"] = map[string]interface{}{
		"conditions": []interface{}{
			map[string]interface{}{
				"type":   "Ready",
				"status": "True",
			},
		},
	}
	return obj
}

// makeLegacyNodeClaim creates a NodeClaim with legacy gpu-provisioner labels.
func makeLegacyNodeClaim(name, wsName, wsNamespace string) *karpenterv1.NodeClaim {
	return &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName:      wsName,
				kaitov1beta1.LabelWorkspaceNamespace: wsNamespace,
			},
		},
	}
}

// makeKarpenterNodeClaim creates a NodeClaim owned by a karpenter NodePool.
func makeKarpenterNodeClaim(name, nodePoolName string, ready bool) *karpenterv1.NodeClaim {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				karpenterv1.NodePoolLabelKey: nodePoolName,
			},
		},
	}
	if ready {
		nc.Status.Conditions = []status.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		}
	}
	return nc
}

func TestKarpenterProvisionerImplementsInterface(t *testing.T) {
	var _ nodeprovision.NodeProvisioner = NewKarpenterProvisioner(newFakeClient(), testConfig)
}

func TestName(t *testing.T) {
	p := NewKarpenterProvisioner(newFakeClient(), testConfig)
	assert.Equal(t, "KarpenterProvisioner", p.Name())
}

func TestStart_CRDNotFound(t *testing.T) {
	// Empty cluster — no CRDs registered.
	c := newFakeClientWithInterceptors(interceptor.Funcs{
		Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*apiextensionsv1.CustomResourceDefinition); ok {
				return apierrors.NewNotFound(schema.GroupResource{Group: "apiextensions.k8s.io", Resource: "customresourcedefinitions"}, key.Name)
			}
			return client.Get(ctx, key, obj, opts...)
		},
	})
	p := NewKarpenterProvisioner(c, testConfig)
	err := p.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Karpenter must be installed")
}

// --- ProvisionNodes tests ---

func TestProvisionNodes_NoBYONodes_CreatesWithFullReplicas(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	c := newFakeClient(nodeClass)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	// Verify NodePool was created with replicas=2.
	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	require.NoError(t, err)
	assert.Equal(t, int64(2), *np.Spec.Replicas)
}

func TestProvisionNodes_DeltaWithBYONodes(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	byoNode := makeReadyNode("byo-1", "Standard_NC24ads_A100_v4", nil)
	c := newFakeClient(nodeClass, byoNode)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	require.NoError(t, err)
	assert.Equal(t, int64(1), *np.Spec.Replicas)
}

func TestProvisionNodes_KarpenterNodesExcludedFromDelta(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	// A karpenter node should NOT count toward non-karpenter coverage.
	karpNode := makeReadyNode("karp-1", "Standard_NC24ads_A100_v4", map[string]string{
		consts.KarpenterWorkspaceNameKey: "ws1",
	})
	c := newFakeClient(nodeClass, karpNode)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	require.NoError(t, err)
	assert.Equal(t, int64(2), *np.Spec.Replicas)
}

func TestProvisionNodes_ZeroReplicasWhenBYOSufficient_NoCreateCalled(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	byo1 := makeReadyNode("byo-1", "Standard_NC24ads_A100_v4", nil)
	byo2 := makeReadyNode("byo-2", "Standard_NC24ads_A100_v4", nil)
	c := newFakeClient(nodeClass, byo1, byo2)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	// No NodePool should be created.
	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	assert.True(t, apierrors.IsNotFound(err))
}

func TestProvisionNodes_DoesNotDecreaseReplicas(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	// 1 BYO appeared after NodePool was created with replicas=2.
	byoNode := makeReadyNode("byo-1", "Standard_NC24ads_A100_v4", nil)
	existingNP := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"},
		Spec:       karpenterv1.NodePoolSpec{Replicas: lo.ToPtr(int64(2))},
	}
	c := newFakeClient(nodeClass, byoNode, existingNP)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	// Replicas should remain 2 (not decrease to 1).
	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	require.NoError(t, err)
	assert.Equal(t, int64(2), *np.Spec.Replicas)
}

func TestProvisionNodes_IncreasesReplicas(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	existingNP := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"},
		Spec:       karpenterv1.NodePoolSpec{Replicas: lo.ToPtr(int64(1))},
	}
	c := newFakeClient(nodeClass, existingNP)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 3, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	require.NoError(t, err)
	assert.Equal(t, int64(3), *np.Spec.Replicas)
}

func TestProvisionNodes_NoUpdateWhenReplicasUnchanged(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	existingNP := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"},
		Spec:       karpenterv1.NodePoolSpec{Replicas: lo.ToPtr(int64(2))},
	}
	c := newFakeClient(nodeClass, existingNP)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	// Replicas unchanged.
	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	require.NoError(t, err)
	assert.Equal(t, int64(2), *np.Spec.Replicas)
}

func TestProvisionNodes_AlreadyExists(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	// Simulate race: Get returns NotFound, Create returns AlreadyExists.
	c := newFakeClientWithInterceptors(interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*karpenterv1.NodePool); ok && key.Name == "default-ws1" {
				return apierrors.NewNotFound(schema.GroupResource{Group: "karpenter.sh", Resource: "nodepools"}, key.Name)
			}
			return cl.Get(ctx, key, obj, opts...)
		},
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*karpenterv1.NodePool); ok {
				return apierrors.NewAlreadyExists(schema.GroupResource{Group: "karpenter.sh", Resource: "nodepools"}, "default-ws1")
			}
			return cl.Create(ctx, obj, opts...)
		},
	}, nodeClass)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	assert.NoError(t, err) // AlreadyExists is silently ignored
}

func TestProvisionNodes_CreateError(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	c := newFakeClientWithInterceptors(interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*karpenterv1.NodePool); ok {
				return errors.New("API server down")
			}
			return cl.Create(ctx, obj, opts...)
		},
	}, nodeClass)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API server down")
}

func TestProvisionNodes_UsesDefaultNodeClassName(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	c := newFakeClient(nodeClass)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	require.NoError(t, err)
	assert.Equal(t, "image-family-ubuntu", np.Spec.Template.Spec.NodeClassRef.Name)
}

func TestProvisionNodes_UsesAnnotationNodeClassName(t *testing.T) {
	nodeClass := makeNodeClassUnstructured("image-family-azure-linux")
	c := newFakeClient(nodeClass)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, map[string]string{
		kaitov1beta1.AnnotationNodeClassName: "image-family-azure-linux",
	})

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	require.NoError(t, err)
	assert.Equal(t, "image-family-azure-linux", np.Spec.Template.Spec.NodeClassRef.Name)
}

// --- DeleteNodes tests ---

func TestDeleteNodes_Success(t *testing.T) {
	np := &karpenterv1.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"}}
	c := newFakeClient(np)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.DeleteNodes(context.Background(), ws)
	assert.NoError(t, err)

	// Verify it's gone.
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, &karpenterv1.NodePool{})
	assert.True(t, apierrors.IsNotFound(err))
}

func TestDeleteNodes_NotFound(t *testing.T) {
	c := newFakeClient() // No NodePool exists.

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.DeleteNodes(context.Background(), ws)
	assert.NoError(t, err) // NotFound is silently ignored
}

func TestDeleteNodes_AlreadyDeleting(t *testing.T) {
	now := metav1.Now()
	np := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "default-ws1",
			DeletionTimestamp: &now,
			Finalizers:        []string{"karpenter.sh/termination"},
		},
	}
	c := newFakeClient(np)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.DeleteNodes(context.Background(), ws)
	assert.NoError(t, err)
	// NodePool should still exist (not deleted again).
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, &karpenterv1.NodePool{})
	assert.NoError(t, err)
}

func TestDeleteNodes_DeleteError(t *testing.T) {
	np := &karpenterv1.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"}}
	c := newFakeClientWithInterceptors(interceptor.Funcs{
		Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*karpenterv1.NodePool); ok {
				return errors.New("forbidden")
			}
			return cl.Delete(ctx, obj, opts...)
		},
	}, np)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	err := p.DeleteNodes(context.Background(), ws)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

// --- EnableDriftRemediation tests ---

func TestEnableDriftRemediation_Success(t *testing.T) {
	np := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"},
		Spec: karpenterv1.NodePoolSpec{
			Disruption: karpenterv1.Disruption{
				ConsolidateAfter: karpenterv1.MustParseNillableDuration("0s"),
				Budgets: []karpenterv1.Budget{
					{
						Nodes:   "0",
						Reasons: []karpenterv1.DisruptionReason{karpenterv1.DisruptionReasonDrifted},
					},
				},
			},
		},
	}
	c := newFakeClient(np)

	p := NewKarpenterProvisioner(c, testConfig)
	err := p.EnableDriftRemediation(context.Background(), "default", "ws1")
	require.NoError(t, err)

	updated := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, updated)
	require.NoError(t, err)
	assert.Equal(t, "1", updated.Spec.Disruption.Budgets[0].Nodes)
}

func TestEnableDriftRemediation_NodePoolNotFound(t *testing.T) {
	c := newFakeClient()

	p := NewKarpenterProvisioner(c, testConfig)
	err := p.EnableDriftRemediation(context.Background(), "default", "ws1")
	assert.Error(t, err)
}

func TestEnableDriftRemediation_NoDriftedBudgetEntry(t *testing.T) {
	np := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"},
		Spec: karpenterv1.NodePoolSpec{
			Disruption: karpenterv1.Disruption{
				ConsolidateAfter: karpenterv1.MustParseNillableDuration("0s"),
				Budgets: []karpenterv1.Budget{
					{
						Nodes:   "10%",
						Reasons: []karpenterv1.DisruptionReason{karpenterv1.DisruptionReasonEmpty},
					},
				},
			},
		},
	}
	c := newFakeClient(np)

	p := NewKarpenterProvisioner(c, testConfig)
	err := p.EnableDriftRemediation(context.Background(), "default", "ws1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no budget entry with Drifted reason")
}

// --- DisableDriftRemediation tests ---

func TestDisableDriftRemediation_Success(t *testing.T) {
	np := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "default-ws1"},
		Spec: karpenterv1.NodePoolSpec{
			Disruption: karpenterv1.Disruption{
				ConsolidateAfter: karpenterv1.MustParseNillableDuration("0s"),
				Budgets: []karpenterv1.Budget{
					{
						Nodes:   "1",
						Reasons: []karpenterv1.DisruptionReason{karpenterv1.DisruptionReasonDrifted},
					},
				},
			},
		},
	}
	c := newFakeClient(np)

	p := NewKarpenterProvisioner(c, testConfig)
	err := p.DisableDriftRemediation(context.Background(), "default", "ws1")
	require.NoError(t, err)

	updated := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, updated)
	require.NoError(t, err)
	assert.Equal(t, "0", updated.Spec.Disruption.Budgets[0].Nodes)
}

func TestDisableDriftRemediation_NodePoolNotFound(t *testing.T) {
	c := newFakeClient()

	p := NewKarpenterProvisioner(c, testConfig)
	err := p.DisableDriftRemediation(context.Background(), "default", "ws1")
	assert.Error(t, err)
}

// --- EnsureNodesReady tests ---

func TestEnsureNodesReady_AllReady_BYOOnly(t *testing.T) {
	byoNode := makeReadyNode("byo-1", "Standard_NC24ads_A100_v4", nil)
	c := newFakeClient(byoNode)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	ready, needRequeue, err := p.EnsureNodesReady(context.Background(), ws)
	require.NoError(t, err)
	assert.True(t, ready)
	assert.False(t, needRequeue)
}

func TestEnsureNodesReady_MixedBYOAndKarpenter(t *testing.T) {
	byoNode := makeReadyNode("byo-1", "Standard_NC24ads_A100_v4", nil)
	karpNode := makeReadyNode("karp-1", "Standard_NC24ads_A100_v4", map[string]string{
		consts.KarpenterWorkspaceNameKey: "ws1",
	})
	// Karpenter NodeClaim — labeled with correct NodePool, ready.
	nc := makeKarpenterNodeClaim("nc-1", "default-ws1", true)
	c := newFakeClient(byoNode, karpNode, nc)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	ready, needRequeue, err := p.EnsureNodesReady(context.Background(), ws)
	require.NoError(t, err)
	assert.True(t, ready)
	assert.False(t, needRequeue)
}

func TestEnsureNodesReady_CountBelowTarget(t *testing.T) {
	c := newFakeClient() // No nodes, no NodeClaims.

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	ready, needRequeue, err := p.EnsureNodesReady(context.Background(), ws)
	require.NoError(t, err)
	assert.False(t, ready)
	assert.False(t, needRequeue)
}

func TestEnsureNodesReady_ListError(t *testing.T) {
	c := newFakeClientWithInterceptors(interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*karpenterv1.NodeClaimList); ok {
				return errors.New("API error")
			}
			return cl.List(ctx, list, opts...)
		},
	})

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	_, _, err := p.EnsureNodesReady(context.Background(), ws)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
}

func TestEnsureNodesReady_DeletingNodeNotCounted(t *testing.T) {
	now := metav1.Now()
	deletingNode := makeReadyNode("deleting-1", "Standard_NC24ads_A100_v4", nil)
	deletingNode.DeletionTimestamp = &now
	deletingNode.Finalizers = []string{"test/finalizer"}
	c := newFakeClient(deletingNode)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	ready, needRequeue, err := p.EnsureNodesReady(context.Background(), ws)
	require.NoError(t, err)
	assert.False(t, ready)
	assert.False(t, needRequeue)
}

// --- countCoveredNodes tests (core new behavior) ---

func TestCountCoveredNodes_NilLabelSelector(t *testing.T) {
	c := newFakeClient()
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)
	ws.Resource.LabelSelector = nil

	covered, ready, err := countCoveredNodes(context.Background(), c, ws)
	require.NoError(t, err)
	assert.Equal(t, 0, covered)
	assert.Equal(t, 0, ready)
}

func TestCountCoveredNodes_LegacyNCCountedDespiteNodeNotReady(t *testing.T) {
	// Core behavior: a non-deleting legacy NodeClaim counts as covered even
	// when its node is not ready (or doesn't exist at all).
	legacyNC := makeLegacyNodeClaim("legacy-nc-1", "ws1", "default")
	c := newFakeClient(legacyNC) // No ready nodes.

	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	covered, readyCount, err := countCoveredNodes(context.Background(), c, ws)
	require.NoError(t, err)
	assert.Equal(t, 1, covered, "legacy NC should count as covered")
	assert.Equal(t, 0, readyCount, "no ready nodes exist")
}

func TestCountCoveredNodes_DeletingLegacyNCExcluded(t *testing.T) {
	now := metav1.Now()
	legacyNC := makeLegacyNodeClaim("legacy-nc-1", "ws1", "default")
	legacyNC.DeletionTimestamp = &now
	legacyNC.Finalizers = []string{"karpenter.sh/termination"}
	c := newFakeClient(legacyNC)

	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	covered, _, err := countCoveredNodes(context.Background(), c, ws)
	require.NoError(t, err)
	assert.Equal(t, 0, covered, "deleting legacy NC should not count as covered")
}

func TestCountCoveredNodes_LegacyNodeExcludedFromBYO(t *testing.T) {
	// A ready node with the legacy workspace label should NOT count as BYO.
	legacyNode := makeReadyNode("legacy-1", "Standard_NC24ads_A100_v4", map[string]string{
		kaitov1beta1.LabelWorkspaceName: "ws1",
	})
	legacyNC := makeLegacyNodeClaim("legacy-nc-1", "ws1", "default")
	c := newFakeClient(legacyNode, legacyNC)

	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	covered, readyCount, err := countCoveredNodes(context.Background(), c, ws)
	require.NoError(t, err)
	// coveredByNonKarpenter = 0 BYO + 1 legacy NC = 1
	assert.Equal(t, 1, covered)
	// readyWithInstanceType = 1 (the legacy node is still a ready node)
	assert.Equal(t, 1, readyCount)
}

func TestCountCoveredNodes_BYOAndLegacyCombinedDelta(t *testing.T) {
	// 1 BYO node + 1 legacy NC (node not ready) + target 3 → covered=2, need 1 karpenter NodeClaim.
	byoNode := makeReadyNode("byo-1", "Standard_NC24ads_A100_v4", nil)
	legacyNC := makeLegacyNodeClaim("legacy-nc-1", "ws1", "default")
	c := newFakeClient(byoNode, legacyNC)

	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 3, nil, nil)

	covered, readyCount, err := countCoveredNodes(context.Background(), c, ws)
	require.NoError(t, err)
	assert.Equal(t, 2, covered, "1 BYO + 1 legacy NC")
	assert.Equal(t, 1, readyCount, "only the BYO node is ready")
}

func TestCountCoveredNodes_LegacyNCsForDifferentWorkspaceNotCounted(t *testing.T) {
	// A legacy NodeClaim for a different workspace should not be counted.
	otherNC := makeLegacyNodeClaim("other-nc", "other-ws", "default")
	c := newFakeClient(otherNC)

	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	covered, _, err := countCoveredNodes(context.Background(), c, ws)
	require.NoError(t, err)
	assert.Equal(t, 0, covered, "NC for different workspace should not count")
}

func TestCountCoveredNodes_KarpenterNodeNotCountedAsBYO(t *testing.T) {
	// A karpenter-labeled node should not count toward BYO coverage.
	karpNode := makeReadyNode("karp-1", "Standard_NC24ads_A100_v4", map[string]string{
		consts.KarpenterWorkspaceNameKey: "ws1",
	})
	c := newFakeClient(karpNode)

	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	covered, readyCount, err := countCoveredNodes(context.Background(), c, ws)
	require.NoError(t, err)
	assert.Equal(t, 0, covered, "karpenter node should not count as non-karpenter coverage")
	assert.Equal(t, 1, readyCount, "karpenter node still counted in total ready")
}

// --- ProvisionNodes delta with legacy NodeClaims ---

func TestProvisionNodes_LegacyNCReducesDelta(t *testing.T) {
	// 1 legacy NC (node not ready) + target 2 → desiredReplicas = 2 - 1 = 1.
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	legacyNC := makeLegacyNodeClaim("legacy-nc-1", "ws1", "default")
	c := newFakeClient(nodeClass, legacyNC)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	require.NoError(t, err)
	assert.Equal(t, int64(1), *np.Spec.Replicas)
}

func TestProvisionNodes_DeletingLegacyNCDoesNotReduceDelta(t *testing.T) {
	// Deleting legacy NC should not count → desiredReplicas stays at target.
	nodeClass := makeNodeClassUnstructured("image-family-ubuntu")
	now := metav1.Now()
	legacyNC := makeLegacyNodeClaim("legacy-nc-1", "ws1", "default")
	legacyNC.DeletionTimestamp = &now
	legacyNC.Finalizers = []string{"karpenter.sh/termination"}
	c := newFakeClient(nodeClass, legacyNC)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 2, nil, nil)

	err := p.ProvisionNodes(context.Background(), ws)
	require.NoError(t, err)

	np := &karpenterv1.NodePool{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "default-ws1"}, np)
	require.NoError(t, err)
	assert.Equal(t, int64(2), *np.Spec.Replicas)
}

// --- CollectNodeStatusInfo tests ---

func TestCollectNodeStatusInfo_AllReady_BYOOnly(t *testing.T) {
	byoNode := makeReadyNode("byo-1", "Standard_NC24ads_A100_v4", nil)
	c := newFakeClient(byoNode)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	conditions, err := p.CollectNodeStatusInfo(context.Background(), ws)
	require.NoError(t, err)
	assert.Equal(t, 3, len(conditions))
	for _, cond := range conditions {
		assert.Equal(t, metav1.ConditionTrue, cond.Status, "condition %s should be True", cond.Type)
	}
}

func TestCollectNodeStatusInfo_NotEnoughNodes(t *testing.T) {
	c := newFakeClient() // No nodes.

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	conditions, err := p.CollectNodeStatusInfo(context.Background(), ws)
	require.NoError(t, err)
	assert.Equal(t, 3, len(conditions))
	for _, cond := range conditions {
		assert.Equal(t, metav1.ConditionFalse, cond.Status, "condition %s should be False", cond.Type)
	}
}

func TestCollectNodeStatusInfo_ListError(t *testing.T) {
	c := newFakeClientWithInterceptors(interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*karpenterv1.NodeClaimList); ok {
				return errors.New("API error")
			}
			return cl.List(ctx, list, opts...)
		},
	})

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	_, err := p.CollectNodeStatusInfo(context.Background(), ws)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API error")
}

func TestCollectNodeStatusInfo_ConditionTypes(t *testing.T) {
	byoNode := makeReadyNode("node-1", "Standard_NC24ads_A100_v4", nil)
	c := newFakeClient(byoNode)

	p := NewKarpenterProvisioner(c, testConfig)
	ws := newTestWorkspace("default", "ws1", "Standard_NC24ads_A100_v4", 1, nil, nil)

	conditions, err := p.CollectNodeStatusInfo(context.Background(), ws)
	require.NoError(t, err)

	typeSet := map[string]bool{}
	for _, cond := range conditions {
		typeSet[cond.Type] = true
	}
	assert.True(t, typeSet[string(kaitov1beta1.ConditionTypeNodeStatus)])
	assert.True(t, typeSet[string(kaitov1beta1.ConditionTypeNodeClaimStatus)])
	assert.True(t, typeSet[string(kaitov1beta1.ConditionTypeResourceStatus)])
}
