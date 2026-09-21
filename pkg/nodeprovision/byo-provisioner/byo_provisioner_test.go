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

package byoprovisioner

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// readyGPUNode builds a Ready node with allocatable GPU capacity and the nvidia.com
// GPU-discovery labels (product/count/per-GPU memory in MiB) plus the instance-type
// label used for SKU selection.
func readyGPUNode(name, instanceType string, perGPUMemMiB, gpuCount int) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				corev1.LabelInstanceTypeStable: instanceType,
				consts.NvidiaGPUProduct:        "Tesla-" + instanceType,
				consts.NvidiaGPUCount:          strconv.Itoa(gpuCount),
				consts.NvidiaGPUMemory:         strconv.Itoa(perGPUMemMiB),
			},
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{"nvidia.com/gpu": *resource.NewQuantity(int64(gpuCount), resource.DecimalSI)},
			Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}
}

func newFakeClient(t *testing.T, nodes ...*corev1.Node) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, n := range nodes {
		builder = builder.WithObjects(n)
	}
	return builder.Build()
}

func TestBuildNodeSelector(t *testing.T) {
	itReq := func(v string) []corev1.NodeSelectorRequirement {
		return []corev1.NodeSelectorRequirement{{
			Key:      corev1.LabelInstanceTypeStable,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{v},
		}}
	}
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "gpu"}}

	tests := []struct {
		name  string
		ws    *kaitov1beta1.Workspace
		nodes []*corev1.Node
		want  []corev1.NodeSelectorRequirement
	}{
		{
			name: "user instanceType -> unconditional hard filter (no node needed)",
			ws:   &kaitov1beta1.Workspace{Resource: kaitov1beta1.ResourceSpec{InstanceType: "Standard_A"}},
			want: itReq("Standard_A"),
		},
		{
			name:  "labelSelector set, no instanceType -> nil (legacy, user scopes nodes)",
			ws:    &kaitov1beta1.Workspace{Resource: kaitov1beta1.ResourceSpec{LabelSelector: selector}},
			nodes: []*corev1.Node{readyGPUNode("n1", "Standard_A", 40000, 1)},
			want:  nil,
		},
		{
			name: "nil labelSelector, no instanceType, no GPU node -> nil",
			ws:   &kaitov1beta1.Workspace{},
			want: nil,
		},
		{
			name: "nil labelSelector, no instanceType -> live-select largest GPUMem SKU",
			ws:   &kaitov1beta1.Workspace{},
			nodes: []*corev1.Node{
				readyGPUNode("small", "Standard_A", 16000, 1),
				readyGPUNode("big", "Standard_B", 80000, 1),
			},
			want: itReq("Standard_B"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := NewBYOProvisioner(newFakeClient(t, tc.nodes...))
			got := n.BuildNodeSelector(context.Background(), tc.ws)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("BuildNodeSelector() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelectInstanceType(t *testing.T) {
	tests := []struct {
		name  string
		nodes []*corev1.Node
		want  string
	}{
		{
			name: "no GPU nodes -> empty (defer to reconcile)",
			want: "",
		},
		{
			name:  "single SKU -> that SKU",
			nodes: []*corev1.Node{readyGPUNode("n1", "Standard_A", 40000, 1)},
			want:  "Standard_A",
		},
		{
			name: "largest per-node GPU memory wins (count x per-GPU mem)",
			nodes: []*corev1.Node{
				readyGPUNode("a", "Standard_A", 40000, 1),
				readyGPUNode("b", "Standard_B", 24000, 2),
			},
			want: "Standard_B",
		},
		{
			name: "tie on GPUMem -> smallest instance-type string",
			nodes: []*corev1.Node{
				readyGPUNode("z", "Standard_Z", 40000, 1),
				readyGPUNode("a", "Standard_A", 40000, 1),
			},
			want: "Standard_A",
		},
		{
			name: "not-ready node ignored",
			nodes: []*corev1.Node{
				func() *corev1.Node {
					n := readyGPUNode("down", "Standard_Big", 80000, 1)
					n.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}
					return n
				}(),
				readyGPUNode("up", "Standard_Small", 16000, 1),
			},
			want: "Standard_Small",
		},
		{
			name: "node without instance-type label ignored",
			nodes: []*corev1.Node{
				func() *corev1.Node {
					n := readyGPUNode("nolabel", "Standard_Big", 80000, 1)
					delete(n.Labels, corev1.LabelInstanceTypeStable)
					return n
				}(),
				readyGPUNode("labeled", "Standard_Small", 16000, 1),
			},
			want: "Standard_Small",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SelectInstanceType(context.Background(), newFakeClient(t, tc.nodes...), &kaitov1beta1.Workspace{})
			if err != nil {
				t.Fatalf("SelectInstanceType() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("SelectInstanceType() = %q, want %q", got, tc.want)
			}
		})
	}
}

func conditionReason(conds []metav1.Condition, condType kaitov1beta1.ConditionType) string {
	for i := range conds {
		if conds[i].Type == string(condType) {
			return conds[i].Reason
		}
	}
	return ""
}

func TestCollectNodeStatusInfo(t *testing.T) {
	tests := []struct {
		name       string
		ws         *kaitov1beta1.Workspace
		nodes      []*corev1.Node
		wantReason string
	}{
		{
			name: "instance type set, no matching node -> NoAvailableGPUNodes",
			ws: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "default"},
				Resource:   kaitov1beta1.ResourceSpec{InstanceType: "Standard_A"},
				Status:     kaitov1beta1.WorkspaceStatus{TargetNodeCount: 1},
			},
			nodes:      []*corev1.Node{readyGPUNode("n1", "Standard_B", 40000, 1)},
			wantReason: "NoAvailableGPUNodes",
		},
		{
			name: "instance type set, matching ready node -> success",
			ws: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "default"},
				Resource:   kaitov1beta1.ResourceSpec{InstanceType: "Standard_A"},
				Status:     kaitov1beta1.WorkspaceStatus{TargetNodeCount: 1},
			},
			nodes:      []*corev1.Node{readyGPUNode("n1", "Standard_A", 40000, 1)},
			wantReason: "workspaceResourceStatusSuccess",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := NewBYOProvisioner(newFakeClient(t, tc.nodes...))
			conds, err := n.CollectNodeStatusInfo(context.Background(), tc.ws)
			if err != nil {
				t.Fatalf("CollectNodeStatusInfo() error = %v", err)
			}
			gotReason := conditionReason(conds, kaitov1beta1.ConditionTypeResourceStatus)
			if gotReason != tc.wantReason {
				t.Errorf("resource condition reason = %q, want %q", gotReason, tc.wantReason)
			}
		})
	}
}
