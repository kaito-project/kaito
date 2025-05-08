// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"context"
	"strings"
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

func TestParseHuggingFaceModelVersion(t *testing.T) {
	tests := []struct {
		name             string
		version          string
		expectedRepoId   string
		expectedRevision string
		expectErr        bool
		expectedErrMsg   string // Use Contains for url.Parse errors, Exact for custom errors
	}{
		{
			name:             "Valid URL with commit revision",
			version:          "https://huggingface.co/tiiuae/falcon-7b/commit/ec89142b67d748a1865ea4451372db8313ada0d8",
			expectedRepoId:   "tiiuae/falcon-7b",
			expectedRevision: "ec89142b67d748a1865ea4451372db8313ada0d8",
			expectErr:        false,
		},
		{
			name:             "Valid URL without commit revision",
			version:          "https://huggingface.co/tiiuae/falcon-7b",
			expectedRepoId:   "tiiuae/falcon-7b",
			expectedRevision: "",
			expectErr:        false,
		},
		{
			name:           "Invalid URL path structure - too few parts",
			version:        "https://huggingface.co/tiiuae",
			expectErr:      true,
			expectedErrMsg: "invalid model version URL: https://huggingface.co/tiiuae. Expected format: https://huggingface.co/<org>/<model>/commit/<revision>",
		},
		{
			name:           "Invalid URL path structure - incorrect middle part",
			version:        "https://huggingface.co/tiiuae/falcon-7b/blob/main",
			expectErr:      true,
			expectedErrMsg: "invalid model version URL: https://huggingface.co/tiiuae/falcon-7b/blob/main. Expected format: https://huggingface.co/<org>/<model>/commit/<revision>",
		},
		{
			name:           "Invalid URL path structure - too many parts",
			version:        "https://huggingface.co/tiiuae/falcon-7b/commit/rev/extra",
			expectErr:      true,
			expectedErrMsg: "invalid model version URL: https://huggingface.co/tiiuae/falcon-7b/commit/rev/extra. Expected format: https://huggingface.co/<org>/<model>/commit/<revision>",
		},
		{
			name:           "Invalid URL format - parsing error",
			version:        "://invalid-url",
			expectErr:      true,
			expectedErrMsg: "missing protocol scheme", // Contains check
		},
		{
			name:             "Valid URL with trailing slash",
			version:          "https://huggingface.co/org/model/",
			expectedRepoId:   "org/model",
			expectedRevision: "",
			expectErr:        false,
		},
		{
			name:             "Valid URL with commit and trailing slash",
			version:          "https://huggingface.co/org/model/commit/revision123/",
			expectedRepoId:   "org/model",
			expectedRevision: "revision123",
			expectErr:        false,
		},
		{
			name:           "Invalid host",
			version:        "https://github.com/org/model",
			expectErr:      true,
			expectedErrMsg: "invalid model version URL: https://github.com/org/model. Expected format: https://huggingface.co/<org>/<model>/commit/<revision>",
		},
		{
			name:           "Empty input string",
			version:        "",
			expectErr:      true,
			expectedErrMsg: "invalid model version URL: . Expected format: https://huggingface.co/<org>/<model>/commit/<revision>", // url.Parse("") returns empty URL, host check fails
		},
		{
			name:           "URL with only host",
			version:        "https://huggingface.co",
			expectErr:      true,
			expectedErrMsg: "invalid model version URL: https://huggingface.co. Expected format: https://huggingface.co/<org>/<model>/commit/<revision>", // Path is empty, len(parts) is 0 or 1 depending on trailing slash
		},
		{
			name:           "URL with path /org/model/tree/branch",
			version:        "https://huggingface.co/org/model/tree/branch",
			expectErr:      true,
			expectedErrMsg: "invalid model version URL: https://huggingface.co/org/model/tree/branch. Expected format: https://huggingface.co/<org>/<model>/commit/<revision>", // len(parts) is 4, but parts[2] is not "commit"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoId, revision, err := ParseHuggingFaceModelVersion(tt.version)

			if tt.expectErr {
				require.Error(t, err)
				if tt.expectedErrMsg != "" {
					// Use Contains for url.Parse errors which might vary slightly, exact match for our custom errors
					if strings.Contains(tt.name, "parsing error") || strings.Contains(tt.name, "Empty input string") {
						assert.Contains(t, err.Error(), tt.expectedErrMsg)
					} else {
						assert.Equal(t, tt.expectedErrMsg, err.Error())
					}
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedRepoId, repoId)
				assert.Equal(t, tt.expectedRevision, revision)
			}
		})
	}
}
