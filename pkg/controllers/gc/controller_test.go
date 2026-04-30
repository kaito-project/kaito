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

package gc

import (
	"context"
	"testing"

	"github.com/awslabs/operatorpkg/status"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	s.AddKnownTypes(gv,
		&karpenterv1.NodeClaim{},
		&karpenterv1.NodeClaimList{},
		&karpenterv1.NodePool{},
		&karpenterv1.NodePoolList{},
	)
	metav1.AddToGroupVersion(s, gv)
	wsGV := schema.GroupVersion{Group: "kaito.sh", Version: "v1beta1"}
	s.AddKnownTypes(wsGV,
		&kaitov1beta1.Workspace{},
		&kaitov1beta1.WorkspaceList{},
	)
	metav1.AddToGroupVersion(s, wsGV)
	coreGV := schema.GroupVersion{Group: "", Version: "v1"}
	s.AddKnownTypes(coreGV,
		&corev1.Pod{},
		&corev1.PodList{},
	)
	metav1.AddToGroupVersion(s, coreGV)
	return s
}

// Helper: create a legacy NodeClaim (gpu-provisioner style).
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

// Helper: create a Karpenter-managed NodeClaim.
func makeKarpenterNodeClaim(name, wsName, wsNamespace string, ready bool) *karpenterv1.NodeClaim {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				consts.KarpenterWorkspaceNameKey:      wsName,
				consts.KarpenterWorkspaceNamespaceKey: wsNamespace,
			},
		},
		Status: karpenterv1.NodeClaimStatus{
			NodeName: "node-" + name,
		},
	}
	if ready {
		nc.Status.Conditions = []status.Condition{
			{
				Type:   "Ready",
				Status: "True",
			},
		}
	}
	return nc
}

// Helper: create a pod running on a given node for the workspace.
func makePodOnNode(name, wsName, wsNamespace, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: wsNamespace,
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName:      wsName,
				kaitov1beta1.LabelWorkspaceNamespace: wsNamespace,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}

// Helper: create a Workspace with Ready status and TargetNodeCount.
func makeReadyWorkspace(name, namespace string, targetNodeCount int32) *kaitov1beta1.Workspace {
	return &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: kaitov1beta1.WorkspaceStatus{
			TargetNodeCount: targetNodeCount,
			Conditions: []metav1.Condition{
				{
					Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
					Status: metav1.ConditionTrue,
				},
			},
		},
	}
}

// Helper: create a Workspace that is NOT ready.
func makeNotReadyWorkspace(name, namespace string, targetNodeCount int32) *kaitov1beta1.Workspace {
	return &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: kaitov1beta1.WorkspaceStatus{
			TargetNodeCount: targetNodeCount,
			Conditions: []metav1.Condition{
				{
					Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
					Status: metav1.ConditionFalse,
				},
			},
		},
	}
}

func newFakeGC(objs ...client.Object) *LegacyNodeClaimGCController {
	s := newTestScheme()
	fc := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		Build()
	return &LegacyNodeClaimGCController{
		client:   fc,
		recorder: record.NewFakeRecorder(10),
	}
}

func TestGC_NoLegacyNodeClaims(t *testing.T) {
	gc := newFakeGC()
	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)
}

func TestGC_LegacyExists_NoKarpenterReplacement(t *testing.T) {
	legacy := makeLegacyNodeClaim("legacy-1", "ws-a", "default")
	ws := makeReadyWorkspace("ws-a", "default", 1)
	gc := newFakeGC(legacy, ws)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Legacy NodeClaim should still exist — no Karpenter replacements.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 1)
}

func TestGC_LegacyExists_KarpenterNotReady(t *testing.T) {
	legacy := makeLegacyNodeClaim("legacy-1", "ws-a", "default")
	karpNC := makeKarpenterNodeClaim("karp-1", "ws-a", "default", false)
	ws := makeReadyWorkspace("ws-a", "default", 1)
	gc := newFakeGC(legacy, karpNC, ws)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Legacy should still exist — Karpenter replacement not ready.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 2)
}

func TestGC_LegacyExists_KarpenterReady_WorkspaceReady_DeletesLegacy(t *testing.T) {
	legacy := makeLegacyNodeClaim("legacy-1", "ws-a", "default")
	karpNC := makeKarpenterNodeClaim("karp-1", "ws-a", "default", true)
	ws := makeReadyWorkspace("ws-a", "default", 1)
	pod := makePodOnNode("pod-1", "ws-a", "default", "node-karp-1")
	gc := newFakeGC(legacy, karpNC, ws, pod)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Legacy should be deleted; only karpenter NodeClaim remains.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 1)
	assert.Equal(t, list.Items[0].Name, "karp-1")
}

func TestGC_WorkspaceNotReady_SkipsGC(t *testing.T) {
	legacy := makeLegacyNodeClaim("legacy-1", "ws-a", "default")
	karpNC := makeKarpenterNodeClaim("karp-1", "ws-a", "default", true)
	ws := makeNotReadyWorkspace("ws-a", "default", 1)
	gc := newFakeGC(legacy, karpNC, ws)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Legacy should still exist — workspace not ready.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 2)
}

func TestGC_KarpenterCountMismatch_SkipsGC(t *testing.T) {
	// Workspace needs 2 nodes, but only 1 Karpenter NodeClaim exists.
	legacy1 := makeLegacyNodeClaim("legacy-1", "ws-a", "default")
	legacy2 := makeLegacyNodeClaim("legacy-2", "ws-a", "default")
	karpNC := makeKarpenterNodeClaim("karp-1", "ws-a", "default", true)
	ws := makeReadyWorkspace("ws-a", "default", 2)
	gc := newFakeGC(legacy1, legacy2, karpNC, ws)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Both legacy should still exist — karpenter count (1) != target (2).
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 3)
}

func TestGC_KarpenterCountMatchesTarget_DeletesLegacy(t *testing.T) {
	// Workspace needs 2 nodes, 2 Karpenter NodeClaims exist and are ready.
	legacy1 := makeLegacyNodeClaim("legacy-1", "ws-a", "default")
	legacy2 := makeLegacyNodeClaim("legacy-2", "ws-a", "default")
	karpNC1 := makeKarpenterNodeClaim("karp-1", "ws-a", "default", true)
	karpNC2 := makeKarpenterNodeClaim("karp-2", "ws-a", "default", true)
	ws := makeReadyWorkspace("ws-a", "default", 2)
	pod1 := makePodOnNode("pod-1", "ws-a", "default", "node-karp-1")
	pod2 := makePodOnNode("pod-2", "ws-a", "default", "node-karp-2")
	gc := newFakeGC(legacy1, legacy2, karpNC1, karpNC2, ws, pod1, pod2)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Both legacy should be deleted.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 2)
	names := make(map[string]bool)
	for _, nc := range list.Items {
		names[nc.Name] = true
	}
	assert.Assert(t, names["karp-1"])
	assert.Assert(t, names["karp-2"])
}

func TestGC_LegacyMissingNamespaceLabel_Skipped(t *testing.T) {
	legacy := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "legacy-no-ns",
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName: "ws-a",
			},
		},
	}
	karpNC := makeKarpenterNodeClaim("karp-1", "ws-a", "default", true)
	ws := makeReadyWorkspace("ws-a", "default", 1)
	gc := newFakeGC(legacy, karpNC, ws)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Legacy should still exist — skipped due to missing namespace label.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 2)
}

func TestGC_MultipleWorkspaces_OnlyReadyOneGCd(t *testing.T) {
	// Workspace A: ready, karpenter ready, count matches, pod on karpenter → should GC legacy.
	legacyA := makeLegacyNodeClaim("legacy-a", "ws-a", "default")
	karpA := makeKarpenterNodeClaim("karp-a", "ws-a", "default", true)
	wsA := makeReadyWorkspace("ws-a", "default", 1)
	podA := makePodOnNode("pod-a", "ws-a", "default", "node-karp-a")

	// Workspace B: karpenter NOT ready → should keep legacy.
	legacyB := makeLegacyNodeClaim("legacy-b", "ws-b", "default")
	karpB := makeKarpenterNodeClaim("karp-b", "ws-b", "default", false)
	wsB := makeReadyWorkspace("ws-b", "default", 1)

	gc := newFakeGC(legacyA, karpA, wsA, podA, legacyB, karpB, wsB)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)

	// Should have 3 remaining: karp-a, legacy-b, karp-b.
	assert.Equal(t, len(list.Items), 3)
	names := make(map[string]bool)
	for _, nc := range list.Items {
		names[nc.Name] = true
	}
	assert.Assert(t, !names["legacy-a"], "legacy-a should have been deleted")
	assert.Assert(t, names["legacy-b"], "legacy-b should still exist")
	assert.Assert(t, names["karp-a"])
	assert.Assert(t, names["karp-b"])
}

func TestGC_KarpenterNodeClaimDeleting_SkipsGC(t *testing.T) {
	legacy := makeLegacyNodeClaim("legacy-1", "ws-a", "default")
	karpNC := makeKarpenterNodeClaim("karp-1", "ws-a", "default", true)
	now := metav1.Now()
	karpNC.DeletionTimestamp = &now
	karpNC.Finalizers = []string{"test-finalizer"}
	ws := makeReadyWorkspace("ws-a", "default", 1)

	gc := newFakeGC(legacy, karpNC, ws)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Legacy should still exist — replacement is deleting.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 2)
}

func TestGC_WorkspaceDeleted_LegacyOrphansStillGCd(t *testing.T) {
	// Workspace no longer exists, but legacy NodeClaims remain alongside ready Karpenter ones.
	legacy := makeLegacyNodeClaim("legacy-1", "ws-gone", "default")
	karpNC := makeKarpenterNodeClaim("karp-1", "ws-gone", "default", true)
	// No Workspace object — simulates deleted workspace.
	gc := newFakeGC(legacy, karpNC)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Legacy should be deleted even though workspace is gone.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 1)
	assert.Equal(t, list.Items[0].Name, "karp-1")
}

func TestGC_MultipleLegacyPerWorkspace_AllDeleted(t *testing.T) {
	legacy1 := makeLegacyNodeClaim("legacy-1", "ws-a", "default")
	legacy2 := makeLegacyNodeClaim("legacy-2", "ws-a", "default")
	karpNC1 := makeKarpenterNodeClaim("karp-1", "ws-a", "default", true)
	karpNC2 := makeKarpenterNodeClaim("karp-2", "ws-a", "default", true)
	ws := makeReadyWorkspace("ws-a", "default", 2)
	pod1 := makePodOnNode("pod-1", "ws-a", "default", "node-karp-1")
	pod2 := makePodOnNode("pod-2", "ws-a", "default", "node-karp-2")
	gc := newFakeGC(legacy1, legacy2, karpNC1, karpNC2, ws, pod1, pod2)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Both legacy NodeClaims should be deleted.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 2)
	names := make(map[string]bool)
	for _, nc := range list.Items {
		names[nc.Name] = true
	}
	assert.Assert(t, names["karp-1"])
	assert.Assert(t, names["karp-2"])
}

func TestGC_PodsStillOnLegacyNode_SkipsGC(t *testing.T) {
	legacy := makeLegacyNodeClaim("legacy-1", "ws-a", "default")
	karpNC := makeKarpenterNodeClaim("karp-1", "ws-a", "default", true)
	ws := makeReadyWorkspace("ws-a", "default", 1)
	// Pod is running on a legacy node, not the Karpenter node.
	pod := makePodOnNode("pod-1", "ws-a", "default", "legacy-node-xyz")
	gc := newFakeGC(legacy, karpNC, ws, pod)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Legacy should still exist — pod not on Karpenter node.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 2)
}

func TestGC_PodsPending_SkipsGC(t *testing.T) {
	legacy := makeLegacyNodeClaim("legacy-1", "ws-a", "default")
	karpNC := makeKarpenterNodeClaim("karp-1", "ws-a", "default", true)
	ws := makeReadyWorkspace("ws-a", "default", 1)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "default",
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName:      "ws-a",
				kaitov1beta1.LabelWorkspaceNamespace: "default",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
	gc := newFakeGC(legacy, karpNC, ws, pod)

	err := gc.runGCCycle(context.Background())
	assert.NilError(t, err)

	// Legacy should still exist — pod is Pending.
	list := &karpenterv1.NodeClaimList{}
	err = gc.client.List(context.Background(), list)
	assert.NilError(t, err)
	assert.Equal(t, len(list.Items), 2)
}
