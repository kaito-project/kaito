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

package nvme

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

// --- NodeNameFromPVAffinity tests ---

func TestNodeNameFromPVAffinity_NilAffinity(t *testing.T) {
	pv := &corev1.PersistentVolume{}
	assert.Equal(t, "", NodeNameFromPVAffinity(pv))
}

func TestNodeNameFromPVAffinity_SingleNode(t *testing.T) {
	pv := &corev1.PersistentVolume{
		Spec: corev1.PersistentVolumeSpec{
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "topology.localdisk.csi.acstor.io/node",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"aks-node-abc123"},
								},
							},
						},
					},
				},
			},
		},
	}
	assert.Equal(t, "aks-node-abc123", NodeNameFromPVAffinity(pv))
}

func TestNodeNameFromPVAffinity_MultipleValues(t *testing.T) {
	pv := &corev1.PersistentVolume{
		Spec: corev1.PersistentVolumeSpec{
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "topology.localdisk.csi.acstor.io/node",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"node-a", "node-b"},
								},
							},
						},
					},
				},
			},
		},
	}
	// Multiple values → not a single-node pin, return empty.
	assert.Equal(t, "", NodeNameFromPVAffinity(pv))
}

// --- CleanupStaleLocalNVMePVCs tests ---

func TestCleanupStaleLocalNVMePVCs_DeletesStalePVC(t *testing.T) {
	mockClient := test.NewClient()

	storageClass := consts.LocalNVMeStorageClass
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "model-weights-volume-ws-0-0",
			Namespace: "default",
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName: "ws-0",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			VolumeName:       "pv-old-node",
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("50Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-old-node",
		},
		Spec: corev1.PersistentVolumeSpec{
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "topology.localdisk.csi.acstor.io/node",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"aks-old-node-gone"},
								},
							},
						},
					},
				},
			},
		},
	}

	pvcMap := mockClient.CreateMapWithType(&corev1.PersistentVolumeClaimList{})
	pvcMap[client.ObjectKeyFromObject(pvc)] = pvc
	mockClient.CreateOrUpdateObjectInMap(pv)

	// List PVCs
	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&corev1.PersistentVolumeClaimList{}), mock.Anything).Return(nil)
	// Get PV — found
	mockClient.On("Get", mock.IsType(context.Background()),
		types.NamespacedName{Name: "pv-old-node"},
		mock.IsType(&corev1.PersistentVolume{}), mock.Anything).Return(nil)
	// Get Node — not found (old node is gone)
	nodeNotFound := k8serrors.NewNotFound(
		schema.GroupResource{Group: "", Resource: "nodes"}, "aks-old-node-gone")
	mockClient.On("Get", mock.IsType(context.Background()),
		types.NamespacedName{Name: "aks-old-node-gone"},
		mock.IsType(&corev1.Node{}), mock.Anything).Return(nodeNotFound)
	// Delete PVC
	mockClient.On("Delete", mock.IsType(context.Background()),
		mock.IsType(&corev1.PersistentVolumeClaim{}), mock.Anything).Return(nil)

	deleted, err := CleanupStaleLocalNVMePVCs(context.Background(), mockClient, "default", "ws-0")
	assert.NilError(t, err)
	assert.Equal(t, 1, deleted)
	mockClient.AssertCalled(t, "Delete", mock.Anything, mock.IsType(&corev1.PersistentVolumeClaim{}), mock.Anything)
}

func TestCleanupStaleLocalNVMePVCs_SkipsWhenNodeExists(t *testing.T) {
	mockClient := test.NewClient()

	storageClass := consts.LocalNVMeStorageClass
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "model-weights-volume-ws-0-0",
			Namespace: "default",
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName: "ws-0",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			VolumeName:       "pv-current-node",
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("50Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-current-node",
		},
		Spec: corev1.PersistentVolumeSpec{
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "topology.localdisk.csi.acstor.io/node",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"aks-current-node"},
								},
							},
						},
					},
				},
			},
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "aks-current-node",
		},
	}

	pvcMap := mockClient.CreateMapWithType(&corev1.PersistentVolumeClaimList{})
	pvcMap[client.ObjectKeyFromObject(pvc)] = pvc
	mockClient.CreateOrUpdateObjectInMap(pv)
	mockClient.CreateOrUpdateObjectInMap(node)

	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&corev1.PersistentVolumeClaimList{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()),
		types.NamespacedName{Name: "pv-current-node"},
		mock.IsType(&corev1.PersistentVolume{}), mock.Anything).Return(nil)
	mockClient.On("Get", mock.IsType(context.Background()),
		types.NamespacedName{Name: "aks-current-node"},
		mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)

	deleted, err := CleanupStaleLocalNVMePVCs(context.Background(), mockClient, "default", "ws-0")
	assert.NilError(t, err)
	assert.Equal(t, 0, deleted)
	mockClient.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything, mock.Anything)
}

func TestCleanupStaleLocalNVMePVCs_SkipsNonNVMePVC(t *testing.T) {
	mockClient := test.NewClient()

	otherClass := "managed-csi"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-pvc",
			Namespace: "default",
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName: "ws-0",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &otherClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("50Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}

	pvcMap := mockClient.CreateMapWithType(&corev1.PersistentVolumeClaimList{})
	pvcMap[client.ObjectKeyFromObject(pvc)] = pvc

	mockClient.On("List", mock.IsType(context.Background()),
		mock.IsType(&corev1.PersistentVolumeClaimList{}), mock.Anything).Return(nil)

	deleted, err := CleanupStaleLocalNVMePVCs(context.Background(), mockClient, "default", "ws-0")
	assert.NilError(t, err)
	assert.Equal(t, 0, deleted)
	mockClient.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything, mock.Anything)
}
