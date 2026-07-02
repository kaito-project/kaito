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

package nodeprovision

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

// fakeProvisioner returns a fixed set of node selector requirements from
// BuildNodeSelector. All other methods are no-ops.
type fakeProvisioner struct {
	reqs []corev1.NodeSelectorRequirement
}

func (f *fakeProvisioner) Name() string                  { return "fake" }
func (f *fakeProvisioner) Start(_ context.Context) error { return nil }
func (f *fakeProvisioner) ProvisionNodes(_ context.Context, _ *kaitov1beta1.Workspace) error {
	return nil
}
func (f *fakeProvisioner) DeleteNodes(_ context.Context, _ *kaitov1beta1.Workspace) error { return nil }
func (f *fakeProvisioner) EnsureNodesReady(_ context.Context, _ *kaitov1beta1.Workspace) (bool, bool, error) {
	return true, false, nil
}
func (f *fakeProvisioner) EnableDriftRemediation(_ context.Context, _, _ string) error  { return nil }
func (f *fakeProvisioner) DisableDriftRemediation(_ context.Context, _, _ string) error { return nil }
func (f *fakeProvisioner) CollectNodeStatusInfo(_ context.Context, _ *kaitov1beta1.Workspace) ([]metav1.Condition, error) {
	return nil, nil
}
func (f *fakeProvisioner) BuildNodeSelector(_ context.Context, _ *kaitov1beta1.Workspace) []corev1.NodeSelectorRequirement {
	return f.reqs
}

func TestGetReadyNodes(t *testing.T) {
	testcases := []struct {
		name                        string
		workspace                   *kaitov1beta1.Workspace
		provisioner                 NodeProvisioner
		nodeList                    *corev1.NodeList
		listNodesError              error
		disableNodeAutoProvisioning bool
		expectedReadyNodes          int
		expectedError               string
		expectedReadyNodeNames      []string
		expectedMatchLabels         client.MatchingLabels
	}{
		{
			name: "successful_retrieval_all_matching_nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"workload": "gpu"},
					},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("ready-node-1", true, false, map[string]string{"workload": "gpu"}),
					createMockNode("ready-node-2", true, false, map[string]string{"workload": "gpu"}),
					createMockNode("ready-node-3", true, false, map[string]string{"workload": "gpu"}),
				},
			},
			expectedReadyNodes:     3,
			expectedReadyNodeNames: []string{"ready-node-1", "ready-node-2", "ready-node-3"},
			expectedMatchLabels:    client.MatchingLabels{"workload": "gpu"},
		},
		{
			name: "nil_provisioner_uses_only_label_selector",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"workload": "gpu"},
					},
				},
			},
			provisioner: nil,
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("ready-node-1", true, false, map[string]string{"workload": "gpu"}),
					createMockNode("ready-node-2", true, false, map[string]string{"workload": "gpu"}),
				},
			},
			expectedReadyNodes:     2,
			expectedReadyNodeNames: []string{"ready-node-1", "ready-node-2"},
			expectedMatchLabels:    client.MatchingLabels{"workload": "gpu"},
		},
		{
			name: "provisioner_requirements_merged_into_filter",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "ws-a", Namespace: "ns-a"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"workload": "gpu"},
					},
				},
			},
			provisioner: &fakeProvisioner{
				reqs: []corev1.NodeSelectorRequirement{
					{Key: kaitov1beta1.LabelWorkspaceName, Operator: corev1.NodeSelectorOpIn, Values: []string{"ws-a"}},
					{Key: kaitov1beta1.LabelWorkspaceNamespace, Operator: corev1.NodeSelectorOpIn, Values: []string{"ns-a"}},
					// Non-In/single-value requirement is ignored.
					{Key: "ignored", Operator: corev1.NodeSelectorOpExists},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("ready-node", true, false, map[string]string{
						"workload":                           "gpu",
						kaitov1beta1.LabelWorkspaceName:      "ws-a",
						kaitov1beta1.LabelWorkspaceNamespace: "ns-a",
					}),
					// Has workload label but is missing the provisioner-stamped
					// workspace labels — must NOT be selected.
					createMockNode("partial-match-node", true, false, map[string]string{
						"workload": "gpu",
					}),
					// Wrong workspace name — must NOT be selected.
					createMockNode("other-workspace-node", true, false, map[string]string{
						"workload":                           "gpu",
						kaitov1beta1.LabelWorkspaceName:      "ws-b",
						kaitov1beta1.LabelWorkspaceNamespace: "ns-a",
					}),
				},
			},
			expectedReadyNodes:     1,
			expectedReadyNodeNames: []string{"ready-node"},
			expectedMatchLabels: client.MatchingLabels{
				"workload":                           "gpu",
				kaitov1beta1.LabelWorkspaceName:      "ws-a",
				kaitov1beta1.LabelWorkspaceNamespace: "ns-a",
			},
		},
		{
			name: "skips_not_ready_nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"workload": "gpu"},
					},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("not-ready-node", false, false, map[string]string{"workload": "gpu"}),
					createMockNode("ready-node", true, false, map[string]string{"workload": "gpu"}),
				},
			},
			expectedReadyNodes:     1,
			expectedReadyNodeNames: []string{"ready-node"},
			expectedMatchLabels:    client.MatchingLabels{"workload": "gpu"},
		},
		{
			name: "skips_deleting_nodes",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"workload": "gpu"},
					},
				},
			},
			nodeList: &corev1.NodeList{
				Items: []corev1.Node{
					createMockNode("deleting-node", true, true, map[string]string{"workload": "gpu"}),
					createMockNode("ready-node", true, false, map[string]string{"workload": "gpu"}),
				},
			},
			expectedReadyNodes:     1,
			expectedReadyNodeNames: []string{"ready-node"},
			expectedMatchLabels:    client.MatchingLabels{"workload": "gpu"},
		},
		{
			name: "empty_node_list",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"workload": "gpu"},
					},
				},
			},
			nodeList:               &corev1.NodeList{Items: []corev1.Node{}},
			expectedReadyNodes:     0,
			expectedReadyNodeNames: []string{},
			expectedMatchLabels:    client.MatchingLabels{"workload": "gpu"},
		},
		{
			name: "list_nodes_fails",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
				Resource: kaitov1beta1.ResourceSpec{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"workload": "gpu"},
					},
				},
			},
			listNodesError: errors.New("failed to list nodes"),
			expectedError:  "failed to list nodes",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			originalFeatureGate := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = tc.disableNodeAutoProvisioning
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalFeatureGate
			}()

			mockClient := test.NewClient()
			if tc.listNodesError != nil {
				mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(tc.listNodesError)
			} else {
				mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					opts := args.Get(2).([]client.ListOption)
					var got client.MatchingLabels
					for _, o := range opts {
						if ml, ok := o.(client.MatchingLabels); ok {
							got = ml
							break
						}
					}
					assert.DeepEqual(t, got, tc.expectedMatchLabels)

					// Emulate apiserver-side label filtering so callers can
					// verify that nodes missing required labels are dropped.
					filtered := &corev1.NodeList{}
					for _, n := range tc.nodeList.Items {
						if matchesAll(n.Labels, got) {
							filtered.Items = append(filtered.Items, n)
						}
					}
					nodeList := args.Get(1).(*corev1.NodeList)
					*nodeList = *filtered
				}).Return(nil)
			}

			readyNodes, err := GetReadyNodes(context.Background(), mockClient, tc.provisioner, tc.workspace)

			if tc.expectedError != "" {
				assert.Check(t, err != nil, "Expected an error")
				assert.Check(t, err.Error() == tc.expectedError, "Expected error message: %s, got: %s", tc.expectedError, err.Error())
				return
			}

			assert.Check(t, err == nil, "Expected no error, got: %v", err)
			assert.Check(t, len(readyNodes) == tc.expectedReadyNodes, "Expected %d ready nodes, got %d", tc.expectedReadyNodes, len(readyNodes))

			actualNames := make([]string, len(readyNodes))
			for i, node := range readyNodes {
				actualNames[i] = node.Name
			}
			assert.DeepEqual(t, actualNames, tc.expectedReadyNodeNames)

			mockClient.AssertExpectations(t)
		})
	}
}

func matchesAll(nodeLabels map[string]string, want client.MatchingLabels) bool {
	for k, v := range want {
		if nodeLabels[k] != v {
			return false
		}
	}
	return true
}

func createMockNode(name string, ready bool, deleting bool, labels map[string]string) corev1.Node {
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{},
		},
	}

	if deleting {
		now := metav1.Now()
		node.ObjectMeta.DeletionTimestamp = &now
	}

	if ready {
		node.Status.Conditions = append(node.Status.Conditions, corev1.NodeCondition{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionTrue,
		})
	} else {
		node.Status.Conditions = append(node.Status.Conditions, corev1.NodeCondition{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionFalse,
		})
	}

	return node
}
