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

package garbagecollect

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/component-helpers/storage/volume"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestReconcile(t *testing.T) {
	// Setup test scheme
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	// Test cases
	tests := []struct {
		name                string
		objects             []client.Object
		expectedRequeue     bool
		expectedRequeueTime time.Duration
		expectedError       bool
		pvDeleted           bool
	}{
		{
			name: "PV without claim reference",
			objects: []client.Object{
				&corev1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pv-without-claim",
					},
					Spec: corev1.PersistentVolumeSpec{
						StorageClassName: consts.LocalNVMeStorageClass,
					},
				},
			},
			expectedRequeue:     false,
			expectedRequeueTime: 0,
			expectedError:       false,
			pvDeleted:           false,
		},
		{
			name: "PV with non-existent PVC",
			objects: []client.Object{
				&corev1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pv-with-missing-pvc",
					},
					Spec: corev1.PersistentVolumeSpec{
						StorageClassName: consts.LocalNVMeStorageClass,
						ClaimRef: &corev1.ObjectReference{
							Namespace: "default",
							Name:      "non-existent-pvc",
						},
					},
				},
			},
			expectedRequeue:     false,
			expectedRequeueTime: 0,
			expectedError:       false,
			pvDeleted:           true,
		},
		{
			name: "PV with existing PVC not being deleted",
			objects: []client.Object{
				&corev1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pv-with-active-pvc",
					},
					Spec: corev1.PersistentVolumeSpec{
						StorageClassName: consts.LocalNVMeStorageClass,
						ClaimRef: &corev1.ObjectReference{
							Namespace: "default",
							Name:      "active-pvc",
						},
					},
				},
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "active-pvc",
						Namespace: "default",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &consts.LocalNVMeStorageClass,
					},
				},
			},
			expectedRequeue:     false,
			expectedRequeueTime: 0,
			expectedError:       false,
			pvDeleted:           false,
		},
		{
			name: "PV with PVC being deleted but node exists",
			objects: []client.Object{
				&corev1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pv-with-deleting-pvc",
					},
					Spec: corev1.PersistentVolumeSpec{
						StorageClassName: consts.LocalNVMeStorageClass,
						ClaimRef: &corev1.ObjectReference{
							Namespace: "default",
							Name:      "deleting-pvc",
						},
					},
				},
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "deleting-pvc",
						Namespace:         "default",
						Finalizers:        []string{"kubernetes.io/pv-protection"},
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
						Annotations: map[string]string{
							volume.AnnSelectedNode: "existing-node",
						},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &consts.LocalNVMeStorageClass,
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "existing-node",
					},
				},
			},
			expectedRequeue:     true,
			expectedRequeueTime: 10 * time.Second,
			expectedError:       false,
			pvDeleted:           false,
		},
		{
			name: "PV with PVC being deleted and node does not exist",
			objects: []client.Object{
				&corev1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "pv-with-orphaned-pvc",
						Finalizers: []string{"kubernetes.io/pv-protection"},
					},
					Spec: corev1.PersistentVolumeSpec{
						StorageClassName: consts.LocalNVMeStorageClass,
						ClaimRef: &corev1.ObjectReference{
							Namespace: "default",
							Name:      "orphaned-pvc",
						},
					},
				},
				&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "orphaned-pvc",
						Namespace:         "default",
						Finalizers:        []string{"kubernetes.io/pv-protection"},
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
						Annotations: map[string]string{
							volume.AnnSelectedNode: "missing-node",
						},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &consts.LocalNVMeStorageClass,
					},
				},
			},
			expectedRequeue:     false,
			expectedRequeueTime: 0,
			expectedError:       false,
			pvDeleted:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake client
			client := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tt.objects...).
				Build()

			// Setup recorder
			recorder := record.NewFakeRecorder(10)

			// Create reconciler
			reconciler := &PersistentVolumeGCReconciler{
				Client:   client,
				Recorder: recorder,
			}

			// Get the PV name from the first object
			pvName := tt.objects[0].(*corev1.PersistentVolume).GetName()

			// Call reconcile
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: pvName,
				},
			}
			result, err := reconciler.Reconcile(context.Background(), req)

			// Check error
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Check requeue
			if tt.expectedRequeue {
				assert.Equal(t, tt.expectedRequeueTime, result.RequeueAfter)
			}

			// Check if PV was deleted
			if tt.pvDeleted {
				pv := &corev1.PersistentVolume{}
				err = client.Get(context.Background(), types.NamespacedName{Name: pvName}, pv)
				assert.True(t, errors.IsNotFound(err), "PV should have been deleted")
			} else {
				pv := &corev1.PersistentVolume{}
				err = client.Get(context.Background(), types.NamespacedName{Name: pvName}, pv)
				assert.NoError(t, err, "PV should still exist")
			}
		})
	}
}

func TestForceDeletePV(t *testing.T) {
	// Setup test scheme
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-pv",
			Finalizers: []string{"kubernetes.io/pv-protection"},
		},
	}

	// Setup fake client
	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(pv).
		Build()

	// Execute forceDeletePV
	err := forceDeletePV(context.Background(), client, pv)
	assert.NoError(t, err)

	// Verify PV has been deleted
	err = client.Get(context.Background(), types.NamespacedName{Name: "test-pv"}, &corev1.PersistentVolume{})
	assert.True(t, errors.IsNotFound(err), "PV should have been deleted")
}
