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

package utils

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
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
		{
			name:        "Node not found",
			nodeNames:   []string{"non-existent-node"},
			nodes:       []runtime.Object{node1, node2},
			expectedGPU: 0,
			expectErr:   true,
			expectedErr: "failed to get node non-existent-node: nodes \"non-existent-node\" not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the fake client with the indexer
			s := scheme.Scheme
			s.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Node{}, &corev1.NodeList{})

			// Build the fake client with the indexer
			kubeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.nodes...).
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

func TestBuildIfElseCmdStr(t *testing.T) {
	tests := []struct {
		name           string
		condition      string
		trueCmd        string
		trueCmdParams  map[string]string
		falseCmd       string
		falseCmdParams map[string]string
		expected       string
	}{
		{
			name:      "Both commands with parameters",
			condition: "[ -f /path/to/file ]",
			trueCmd:   "echo 'File exists'",
			trueCmdParams: map[string]string{
				"flag1": "",
			},
			falseCmd: "echo 'File does not exist'",
			falseCmdParams: map[string]string{
				"param2": "value2",
			},
			expected: "if [ -f /path/to/file ]; then echo 'File exists' --flag1; else echo 'File does not exist' --param2=value2; fi",
		},
		{
			name:      "True command with parameters, false command without",
			condition: "check_condition",
			trueCmd:   "do_true_action",
			trueCmdParams: map[string]string{
				"arg": "true_arg",
			},
			falseCmd:       "do_false_action",
			falseCmdParams: map[string]string{},
			expected:       "if check_condition; then do_true_action --arg=true_arg; else do_false_action; fi",
		},
		{
			name:           "False command with parameters, true command without",
			condition:      "another_check",
			trueCmd:        "simple_true",
			trueCmdParams:  map[string]string{},
			falseCmd:       "complex_false",
			falseCmdParams: map[string]string{"opt": "false_opt"},
			expected:       "if another_check; then simple_true; else complex_false --opt=false_opt; fi",
		},
		{
			name:           "Neither command with parameters",
			condition:      "basic_test",
			trueCmd:        "run_if_true",
			trueCmdParams:  map[string]string{},
			falseCmd:       "run_if_false",
			falseCmdParams: map[string]string{},
			expected:       "if basic_test; then run_if_true; else run_if_false; fi",
		},
		{
			name:           "Empty true command",
			condition:      "test_empty_true",
			trueCmd:        "",
			trueCmdParams:  map[string]string{},
			falseCmd:       "fallback",
			falseCmdParams: map[string]string{"p": "v"},
			expected:       "if test_empty_true; then ; else fallback --p=v; fi",
		},
		{
			name:      "Empty false command",
			condition: "test_empty_false",
			trueCmd:   "primary",
			trueCmdParams: map[string]string{
				"a": "b",
			},
			falseCmd:       "",
			falseCmdParams: map[string]string{},
			expected:       "if test_empty_false; then primary --a=b; else ; fi",
		},
		{
			name:           "Empty condition",
			condition:      "",
			trueCmd:        "true_cmd",
			trueCmdParams:  map[string]string{},
			falseCmd:       "false_cmd",
			falseCmdParams: map[string]string{},
			expected:       "if ; then true_cmd; else false_cmd; fi",
		},
		{
			name:           "Nil parameters map",
			condition:      "nil_test",
			trueCmd:        "true_cmd",
			trueCmdParams:  nil,
			falseCmd:       "false_cmd",
			falseCmdParams: nil,
			expected:       "if nil_test; then true_cmd; else false_cmd; fi",
		},
		{
			name:          "Nil true parameters map",
			condition:     "nil_true_params",
			trueCmd:       "true_cmd",
			trueCmdParams: nil,
			falseCmd:      "false_cmd",
			falseCmdParams: map[string]string{
				"f_param": "f_val",
			},
			expected: "if nil_true_params; then true_cmd; else false_cmd --f_param=f_val; fi",
		},
		{
			name:      "Nil false parameters map",
			condition: "nil_false_params",
			trueCmd:   "true_cmd",
			trueCmdParams: map[string]string{
				"t_param": "t_val",
			},
			falseCmd:       "false_cmd",
			falseCmdParams: nil,
			expected:       "if nil_false_params; then true_cmd --t_param=t_val; else false_cmd; fi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := BuildIfElseCmdStr(tt.condition, tt.trueCmd, tt.trueCmdParams, tt.falseCmd, tt.falseCmdParams)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestGetRayLeaderHost(t *testing.T) {
	tests := []struct {
		name         string
		meta         metav1.ObjectMeta
		expectedHost string
	}{
		{
			name: "Standard case",
			meta: metav1.ObjectMeta{
				Name:      "my-ray-cluster",
				Namespace: "default",
			},
			expectedHost: "my-ray-cluster-0.my-ray-cluster-headless.default.svc.cluster.local",
		},
		{
			name: "Different name and namespace",
			meta: metav1.ObjectMeta{
				Name:      "another-app",
				Namespace: "kube-system",
			},
			expectedHost: "another-app-0.another-app-headless.kube-system.svc.cluster.local",
		},
		{
			name: "Name with hyphens",
			meta: metav1.ObjectMeta{
				Name:      "test-ray-app-v1",
				Namespace: "production",
			},
			expectedHost: "test-ray-app-v1-0.test-ray-app-v1-headless.production.svc.cluster.local",
		},
		{
			name: "Namespace with hyphens",
			meta: metav1.ObjectMeta{
				Name:      "simple",
				Namespace: "my-custom-namespace",
			},
			expectedHost: "simple-0.simple-headless.my-custom-namespace.svc.cluster.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualHost := GetRayLeaderHost(tt.meta)
			assert.Equal(t, tt.expectedHost, actualHost)
		})
	}
}

func TestInferencePoolName(t *testing.T) {
	tests := []struct {
		workspaceName string
		expected      string
	}{
		{"foo", "foo-inferencepool"},
		{"bar123", "bar123-inferencepool"},
		{"test-workspace", "test-workspace-inferencepool"},
	}

	for _, tt := range tests {
		t.Run(tt.workspaceName, func(t *testing.T) {
			actual := InferencePoolName(tt.workspaceName)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestClientObjectSpecEqual(t *testing.T) {
	i32 := func(n int32) *int32 { return &n }

	tests := []struct {
		name string
		a, b client.Object
		want bool
	}{
		{
			name: "equal deployment specs despite different metadata",
			a: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns1"},
				Spec: appsv1.DeploymentSpec{
					Replicas: i32(2),
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "demo"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c1", Image: "busybox:latest"}}},
					},
				},
			},
			b: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns2", Labels: map[string]string{"x": "y"}},
				Spec: appsv1.DeploymentSpec{
					Replicas: i32(2),
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "demo"}},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c1", Image: "busybox:latest"}}},
					},
				},
			},
			want: true,
		},
		{
			name: "different deployment specs (replicas)",
			a: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"},
				Spec:       appsv1.DeploymentSpec{Replicas: i32(2)},
			},
			b: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns"},
				Spec:       appsv1.DeploymentSpec{Replicas: i32(3)},
			},
			want: false,
		},
		{
			name: "equal service specs with different names",
			a: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
					Selector: map[string]string{"app": "demo"},
				},
			},
			b: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "ns"},
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
					Selector: map[string]string{"app": "demo"},
				},
			},
			want: true,
		},
		{
			name: "one object without spec (ConfigMap) vs Service",
			a:    &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"}},
			b: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "ns"},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
			},
			want: false,
		},
		{
			name: "both objects without spec (ConfigMap vs ConfigMap)",
			a:    &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-a", Namespace: "ns"}},
			b:    &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm-b", Namespace: "ns"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ClientObjectSpecEqual(tt.a, tt.b)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
