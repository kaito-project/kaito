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

package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

// gaugeValue reads the current float64 value from a prometheus.GaugeVec for the given label values.
func gaugeValue(g *prometheus.GaugeVec, lvs ...string) float64 {
	gauge, err := g.GetMetricWithLabelValues(lvs...)
	if err != nil {
		return 0
	}
	pb := &dto.Metric{}
	if err := gauge.Write(pb); err != nil {
		return 0
	}
	return pb.GetGauge().GetValue()
}

// gaugeCount returns the number of label-set entries currently tracked by a GaugeVec.
func gaugeCount(g *prometheus.GaugeVec) int {
	ch := make(chan prometheus.Metric, 100)
	go func() {
		g.Collect(ch)
		close(ch)
	}()
	count := 0
	for range ch {
		count++
	}
	return count
}

func TestGetWorkspacePresetName(t *testing.T) {
	tests := []struct {
		name     string
		ws       *kaitov1beta1.Workspace
		expected string
	}{
		{
			name: "workspace with preset returns preset name",
			ws: &kaitov1beta1.Workspace{
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "phi-4",
						},
					},
				},
			},
			expected: "phi-4",
		},
		{
			name: "workspace with custom template returns empty string",
			ws: &kaitov1beta1.Workspace{
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: nil,
				},
			},
			expected: "",
		},
		{
			name:     "workspace with empty inference spec returns empty string",
			ws:       &kaitov1beta1.Workspace{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getWorkspacePresetName(tt.ws)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetermineWorkspacePhase(t *testing.T) {
	tests := []struct {
		name     string
		ws       *kaitov1beta1.Workspace
		expected string
	}{
		{
			name: "workspace is deleting",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.WorkspaceConditionTypeDeleting),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: "deleting",
		},
		{
			name: "workspace succeeded",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: "succeeded",
		},
		{
			name: "workspace error",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			expected: "error",
		},
		{
			name: "workspace pending with no conditions",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expected: "pending",
		},
		{
			name: "workspace pending with unknown condition",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "UnknownCondition",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: "pending",
		},
		{
			name: "deleting takes precedence over succeeded",
			ws: &kaitov1beta1.Workspace{
				Status: kaitov1beta1.WorkspaceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kaitov1beta1.WorkspaceConditionTypeDeleting),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(kaitov1beta1.WorkspaceConditionTypeSucceeded),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expected: "deleting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetermineWorkspacePhase(tt.ws)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollectPVCMetrics(t *testing.T) {
	tests := []struct {
		name       string
		setupMocks func(c *test.MockClient)
		validate   func(t *testing.T)
	}{
		{
			name: "PVC with workspace label and status capacity emits allocated bytes",
			setupMocks: func(c *test.MockClient) {
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "model-weights-volume-ws1-0",
						Namespace: "default",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName: "ws1",
						},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("100Gi"),
							},
						},
					},
					Status: corev1.PersistentVolumeClaimStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Gi"),
						},
					},
				}
				relevantMap := c.CreateMapWithType(&corev1.PersistentVolumeClaimList{})
				relevantMap[client.ObjectKeyFromObject(pvc)] = pvc
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.PersistentVolumeClaimList{}), mock.Anything).Return(nil)
			},
			validate: func(t *testing.T) {
				// 100Gi = 107374182400 bytes
				allocatedBytes := gaugeValue(workspacePVCAllocatedBytes, "ws1", "default", "model-weights-volume-ws1-0")
				assert.Equal(t, float64(107374182400), allocatedBytes, "allocated bytes should be 100Gi in bytes")

				pvcCount := gaugeValue(workspacePVCCount, "ws1", "default")
				assert.Equal(t, float64(1), pvcCount, "PVC count should be 1")
			},
		},
		{
			name: "PVC without workspace label is skipped",
			setupMocks: func(c *test.MockClient) {
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "unrelated-pvc",
						Namespace: "default",
						Labels:    map[string]string{},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("50Gi"),
							},
						},
					},
				}
				relevantMap := c.CreateMapWithType(&corev1.PersistentVolumeClaimList{})
				relevantMap[client.ObjectKeyFromObject(pvc)] = pvc
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.PersistentVolumeClaimList{}), mock.Anything).Return(nil)
			},
			validate: func(t *testing.T) {
				assert.Equal(t, 0, gaugeCount(workspacePVCAllocatedBytes), "expected no allocated bytes metrics")
				assert.Equal(t, 0, gaugeCount(workspacePVCCount), "expected no PVC count metrics")
			},
		},
		{
			name: "PVC without status capacity falls back to spec request",
			setupMocks: func(c *test.MockClient) {
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "model-weights-volume-ws2-0",
						Namespace: "gpu-ns",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName: "ws2",
						},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("200Gi"),
							},
						},
					},
					// No Status.Capacity — PVC is pending
				}
				relevantMap := c.CreateMapWithType(&corev1.PersistentVolumeClaimList{})
				relevantMap[client.ObjectKeyFromObject(pvc)] = pvc
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.PersistentVolumeClaimList{}), mock.Anything).Return(nil)
			},
			validate: func(t *testing.T) {
				// 200Gi = 214748364800 bytes
				allocatedBytes := gaugeValue(workspacePVCAllocatedBytes, "ws2", "gpu-ns", "model-weights-volume-ws2-0")
				assert.Equal(t, float64(214748364800), allocatedBytes, "allocated bytes should be 200Gi in bytes (from spec request fallback)")

				pvcCount := gaugeValue(workspacePVCCount, "ws2", "gpu-ns")
				assert.Equal(t, float64(1), pvcCount, "PVC count should be 1")
			},
		},
		{
			name: "Multiple PVCs for same workspace are counted correctly",
			setupMocks: func(c *test.MockClient) {
				pvc1 := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "model-weights-volume-ws3-0",
						Namespace: "default",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName: "ws3",
						},
					},
					Status: corev1.PersistentVolumeClaimStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Gi"),
						},
					},
				}
				pvc2 := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "model-weights-volume-ws3-1",
						Namespace: "default",
						Labels: map[string]string{
							kaitov1beta1.LabelWorkspaceName: "ws3",
						},
					},
					Status: corev1.PersistentVolumeClaimStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Gi"),
						},
					},
				}
				relevantMap := c.CreateMapWithType(&corev1.PersistentVolumeClaimList{})
				relevantMap[client.ObjectKeyFromObject(pvc1)] = pvc1
				relevantMap[client.ObjectKeyFromObject(pvc2)] = pvc2
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.PersistentVolumeClaimList{}), mock.Anything).Return(nil)
			},
			validate: func(t *testing.T) {
				pvcCount := gaugeValue(workspacePVCCount, "ws3", "default")
				assert.Equal(t, float64(2), pvcCount, "PVC count should be 2")

				allocatedBytes0 := gaugeValue(workspacePVCAllocatedBytes, "ws3", "default", "model-weights-volume-ws3-0")
				assert.Equal(t, float64(107374182400), allocatedBytes0, "PVC 0 allocated bytes should be 100Gi")

				allocatedBytes1 := gaugeValue(workspacePVCAllocatedBytes, "ws3", "default", "model-weights-volume-ws3-1")
				assert.Equal(t, float64(107374182400), allocatedBytes1, "PVC 1 allocated bytes should be 100Gi")
			},
		},
		{
			name: "List error resets all PVC metrics",
			setupMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.PersistentVolumeClaimList{}), mock.Anything).Return(errors.New("api error"))
			},
			validate: func(t *testing.T) {
				assert.Equal(t, 0, gaugeCount(workspacePVCAllocatedBytes), "expected no allocated bytes metrics after error")
				assert.Equal(t, 0, gaugeCount(workspacePVCCount), "expected no PVC count metrics after error")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset metrics before each test
			workspacePVCAllocatedBytes.Reset()
			workspacePVCCount.Reset()

			mockClient := test.NewClient()
			tt.setupMocks(mockClient)

			collectPVCMetrics(context.Background(), mockClient)

			tt.validate(t)
		})
	}
}
