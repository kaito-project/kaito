// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFetchGPUCountFromNodes(t *testing.T) {
	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("2"),
			},
		},
	}

	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-2",
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("4"),
			},
		},
	}

	node3 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-3",
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{},
		},
	}

	tests := []struct {
		name        string
		nodeNames   []string
		nodes       []runtime.Object
		expectedGPU int
		expectErr   bool
		expectedErr string
	}{
		{
			name:        "Single Node with GPU",
			nodeNames:   []string{"node-1"},
			nodes:       []runtime.Object{node1},
			expectedGPU: 2,
		},
		{
			name:        "Multiple Nodes with GPU",
			nodeNames:   []string{"node-1", "node-2"},
			nodes:       []runtime.Object{node1, node2},
			expectedGPU: 2,
		},
		{
			name:        "Node without GPU",
			nodeNames:   []string{"node-3"},
			nodes:       []runtime.Object{node3},
			expectedGPU: 0,
		},
		{
			name:        "No Worker Nodes",
			nodeNames:   []string{},
			nodes:       []runtime.Object{},
			expectedGPU: 0,
			expectErr:   true,
			expectedErr: "no worker nodes found in the workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the fake client with the indexer
			s := scheme.Scheme
			s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Node{}, &corev1.NodeList{})

			// Create an indexer function for the "metadata.name" field
			indexFunc := func(obj client.Object) []string {
				return []string{obj.(*corev1.Node).Name}
			}

			// Build the fake client with the indexer
			kubeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.nodes...).
				WithIndex(&corev1.Node{}, "metadata.name", indexFunc).
				Build()

			// Call the function
			gpuCount, err := FetchGPUCountFromNodes(context.TODO(), kubeClient, tt.nodeNames)

			// Check the error
			if tt.expectErr {
				require.Error(t, err)
				assert.Equal(t, tt.expectedErr, err.Error())
			} else {
				require.NoError(t, err)
			}

			// Check the GPU count
			assert.Equal(t, tt.expectedGPU, gpuCount)
		})
	}
}
