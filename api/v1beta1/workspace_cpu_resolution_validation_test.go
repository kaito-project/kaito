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

package v1beta1

import (
	"context"
	"fmt"
	goruntime "runtime"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// cpuCreateValidationClient separates CPU target detection from the resource
// validator's existing GPU inventory lookup, even though both list Nodes.
type cpuCreateValidationClient struct {
	client.Client
	cpuLists int
	gpuLists int
	listErr  error
}

func (c *cpuCreateValidationClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*corev1.NodeList); ok {
		cpuDetection := false
		for depth := 1; ; depth++ {
			pc, _, _, ok := goruntime.Caller(depth)
			if !ok {
				break
			}
			if fn := goruntime.FuncForPC(pc); fn != nil && strings.HasSuffix(fn.Name(), ".AreLabelSelectedReadyNodesCPU") {
				cpuDetection = true
				break
			}
		}
		if cpuDetection {
			c.cpuLists++
		} else {
			c.gpuLists++
		}
		if c.listErr != nil {
			return c.listErr
		}
	}
	return c.Client.List(ctx, list, opts...)
}

func setupCPUCreateValidation(t *testing.T, nodes ...client.Object) *cpuCreateValidationClient {
	t.Helper()
	registerCPUValidationTestModels()
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	oldMIG := featuregates.FeatureGates[consts.FeatureFlagEnableMIG]
	featuregates.FeatureGates[consts.FeatureFlagEnableMIG] = true
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagEnableMIG] = oldMIG })
	oldNAP, oldVLLM, oldClient := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning], featuregates.FeatureGates[consts.FeatureFlagVLLM], k8sclient.Client
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning], featuregates.FeatureGates[consts.FeatureFlagVLLM], k8sclient.Client = oldNAP, oldVLLM, oldClient
	})
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning], featuregates.FeatureGates[consts.FeatureFlagVLLM] = true, true
	c := &cpuCreateValidationClient{Client: newCPUValidationClient(t, nodes...)}
	k8sclient.Client = c
	return c
}

func cpuCreateValidationWorkspace() *Workspace {
	return &Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu", Namespace: "default"},
		Resource:   ResourceSpec{Count: pointerToInt(1), LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cpu"}}},
		Inference:  &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "phi-3-mini-4k-instruct"}}},
	}
}

func cpuCreateValidationNode(name string, gpu bool) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"pool": "cpu"}},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	}
	if gpu {
		node.Labels[corev1.LabelInstanceTypeStable] = "Standard_NC24ads_A100_v4"
	}
	return node
}

func TestValidateCreateCPUResolutionReuse(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		nodes                      []client.Object
		listErr                    error
		mutate                     func(*Workspace)
		wantCPULists, wantGPULists int
		wantErrors                 []string
	}{
		{name: "label-only CPU skips GPU validation", nodes: []client.Object{cpuCreateValidationNode("cpu", false)}, wantCPULists: 1},
		{name: "GPU retains independent inventory validation", nodes: []client.Object{cpuCreateValidationNode("gpu", true)}, wantCPULists: 1, wantGPULists: 1},
		{name: "mixed nodes rejected without GPU lookup", nodes: []client.Object{cpuCreateValidationNode("cpu", false), cpuCreateValidationNode("gpu", true)}, wantCPULists: 1, wantErrors: []string{"mix of CPU-only and GPU nodes", "resource.labelSelector"}},
		{name: "resolution failure preserves independent errors", listErr: fmt.Errorf("inventory unavailable"), wantCPULists: 1, mutate: func(w *Workspace) {
			w.Annotations = map[string]string{AnnotationPerformanceMode: "invalid"}
			w.Inference.Config = "missing-config"
		}, wantErrors: []string{"failed to resolve CPU inference target: inventory unavailable", "resource.labelSelector", "not a valid performance mode", "missing-config"}},
		{name: "CPU rejects partitions", nodes: []client.Object{cpuCreateValidationNode("cpu", false)}, wantCPULists: 1, mutate: func(w *Workspace) { w.Resource.Partition = &PartitionSpec{Mode: PartitionModeMIG, Profile: "1g.10gb"} }, wantErrors: []string{"basic CPU inference does not support resource.partition", "resource.resource.partition"}},
		{name: "invalid preset does not list", mutate: func(w *Workspace) { w.Inference.Preset.Name = "not-a-preset" }, wantErrors: []string{"Unsupported inference preset name"}},
		{name: "malformed selector avoids all node lookups", listErr: fmt.Errorf("inventory unavailable"), mutate: func(w *Workspace) {
			w.Resource.LabelSelector.MatchExpressions = []metav1.LabelSelectorRequirement{{Key: "pool", Operator: metav1.LabelSelectorOpExists}}
		}, wantErrors: []string{"cannot be converted into the old label selector format", "resource.labelSelector"}},
		{name: "reserved label is sanitized alongside usable selector", nodes: []client.Object{cpuCreateValidationNode("cpu", false)}, wantCPULists: 1, mutate: func(w *Workspace) { w.Resource.LabelSelector.MatchLabels[LabelWorkspaceName] = "other" }},
		{name: "reserved-only selector does not list", mutate: func(w *Workspace) {
			w.Resource.LabelSelector.MatchLabels = map[string]string{LabelWorkspaceName: "other"}
		}, wantErrors: []string{"matchLabels must contain at least one non-reserved label", "resource.labelSelector.matchLabels"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := setupCPUCreateValidation(t, tc.nodes...)
			c.listErr = tc.listErr
			w := cpuCreateValidationWorkspace()
			if tc.mutate != nil {
				tc.mutate(w)
			}
			errs := w.ValidateCreate(context.Background())
			if len(tc.wantErrors) == 0 && errs != nil {
				t.Fatalf("unexpected errors: %v", errs)
			}
			for _, want := range tc.wantErrors {
				if errs == nil || !strings.Contains(errs.Error(), want) {
					t.Errorf("expected %q, got %v", want, errs)
				}
			}
			if c.cpuLists != tc.wantCPULists || c.gpuLists != tc.wantGPULists {
				t.Errorf("CPU/GPU Lists = %d/%d, want %d/%d", c.cpuLists, c.gpuLists, tc.wantCPULists, tc.wantGPULists)
			}
		})
	}
}

func TestCPUValidationStandaloneResolution(t *testing.T) {
	c := setupCPUCreateValidation(t, cpuCreateValidationNode("cpu", false))
	w := cpuCreateValidationWorkspace()
	ctx := context.Background()
	if errs := w.validateCPUInferenceConstraints(ctx, c); errs != nil {
		t.Fatal(errs)
	}
	if errs := w.Resource.validateCreateWithInference(ctx, w.Inference, false, model.RuntimeNameVLLM, w.Namespace); errs != nil {
		t.Fatal(errs)
	}
	if c.cpuLists != 2 || c.gpuLists != 0 {
		t.Fatalf("independent validators should each resolve CPU, got %d/%d Lists", c.cpuLists, c.gpuLists)
	}
	// Neither a separate create nor a standalone call may reuse stale inventory.
	c.listErr = fmt.Errorf("inventory changed")
	for _, errs := range []*apis.FieldError{w.ValidateCreate(ctx), w.validateCPUInferenceConstraints(ctx, c), w.Resource.validateCreateWithInference(ctx, w.Inference, false, model.RuntimeNameVLLM, w.Namespace)} {
		if errs == nil || !strings.Contains(errs.Error(), "inventory changed") {
			t.Errorf("expected fresh resolution failure, got %v", errs)
		}
	}
	if c.cpuLists != 5 || c.gpuLists != 0 {
		t.Fatalf("expected one fresh CPU resolution per entrypoint, got %d/%d Lists", c.cpuLists, c.gpuLists)
	}
}

func TestCPUResourceValidationEarlyReturns(t *testing.T) {
	for _, malformedSelector := range []bool{false, true} {
		t.Run(fmt.Sprintf("malformedSelector=%t", malformedSelector), func(t *testing.T) {
			c := setupCPUCreateValidation(t)
			w := cpuCreateValidationWorkspace()
			want := ""
			if malformedSelector {
				w.Resource.LabelSelector.MatchExpressions = []metav1.LabelSelectorRequirement{{Key: "pool", Operator: metav1.LabelSelectorOpExists}}
				want = "cannot be converted into the old label selector format"
			} else {
				w.Inference.Preset.Name = "not-a-preset"
			}
			errs := w.Resource.validateCreateWithInference(context.Background(), w.Inference, false, model.RuntimeNameVLLM, w.Namespace)
			if (want == "" && errs != nil) || (want != "" && (errs == nil || !strings.Contains(errs.Error(), want))) {
				t.Fatalf("expected %q, got %v", want, errs)
			}
			if c.cpuLists != 0 || c.gpuLists != 0 {
				t.Fatalf("early return must not list nodes: %d/%d", c.cpuLists, c.gpuLists)
			}
		})
	}
}

func TestValidateCreateCPURuntimeAndCount(t *testing.T) {
	for _, tc := range []struct {
		name, runtime, instanceType string
		count                       *int
		disableVLLM                 bool
		wantErrors                  []string
	}{
		{name: "nil count defaults to one"},
		{name: "one", count: pointerToInt(1)},
		{name: "multiple nodes", count: pointerToInt(2), wantErrors: []string{"single node", "resource.count"}},
		{name: "zero count", count: pointerToInt(0), wantErrors: []string{"single node", "resource.count"}},
		{name: "invalid runtime and count", runtime: "bogus", count: pointerToInt(2), wantErrors: []string{"CPU inference requires vllm or explicit transformers", "metadata.annotations[" + AnnotationWorkspaceRuntime + "]", "single node", "resource.count"}},
		{name: "disabled vllm gate", disableVLLM: true, wantErrors: []string{"vllm feature gate", "inference.preset"}},
		{name: "label-only transformers not explicit declared CPU", runtime: "transformers", wantErrors: []string{"CPU inference requires vllm or explicit transformers"}},
		{name: "explicit transformers CPU exception", runtime: "transformers", instanceType: "Standard_D4s_v3", disableVLLM: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := setupCPUCreateValidation(t, cpuCreateValidationNode("cpu", false))
			featuregates.FeatureGates[consts.FeatureFlagVLLM] = !tc.disableVLLM
			w := cpuCreateValidationWorkspace()
			w.Resource.Count, w.Resource.InstanceType = tc.count, tc.instanceType
			if tc.runtime != "" {
				w.Annotations = map[string]string{AnnotationWorkspaceRuntime: tc.runtime}
			}
			errs := w.ValidateCreate(context.Background())
			if len(tc.wantErrors) == 0 && errs != nil {
				t.Fatalf("unexpected errors: %v", errs)
			}
			for _, want := range tc.wantErrors {
				if errs == nil || !strings.Contains(errs.Error(), want) {
					t.Errorf("expected %q, got %v", want, errs)
				}
			}
			if tc.runtime == "bogus" && errs != nil && strings.Contains(errs.Error(), "vllm feature gate") {
				t.Errorf("invalid runtime must not be reported as feature gate failure: %v", errs)
			}
			wantCPULists := 1
			if tc.instanceType != "" {
				wantCPULists = 0
			}
			if c.cpuLists != wantCPULists || c.gpuLists != 0 {
				t.Errorf("CPU/GPU Lists = %d/%d, want %d/0", c.cpuLists, c.gpuLists, wantCPULists)
			}
		})
	}
}

func TestValidateCreateCPUResolutionNotUsedOnUpdate(t *testing.T) {
	c := setupCPUCreateValidation(t)
	c.listErr = fmt.Errorf("inventory unavailable")
	w := cpuCreateValidationWorkspace()
	updated := w.DeepCopy()
	updated.Labels = map[string]string{"owner": "team"}
	if errs := updated.ValidateUpdate(context.Background(), w); errs != nil {
		t.Fatalf("ordinary update failed: %v", errs)
	}
	if c.cpuLists != 0 || c.gpuLists != 0 {
		t.Fatalf("ordinary update must not list nodes: %d/%d", c.cpuLists, c.gpuLists)
	}
}

func TestCPUNodeClassificationKeepsReadinessAndInstanceFilter(t *testing.T) {
	for _, tc := range []struct {
		name               string
		mutate             func(*corev1.Node)
		instanceType       string
		wantCPU, wantError bool
	}{
		{name: "AWS CPU rejected", mutate: func(n *corev1.Node) { n.Spec.ProviderID = "aws:///zone/node" }, wantError: true},
		{name: "AWS GPU is GPU not unsupported CPU", mutate: func(n *corev1.Node) {
			n.Spec.ProviderID = "aws:///zone/node"
			n.Labels[corev1.LabelInstanceTypeStable] = "Standard_NC24ads_A100_v4"
		}},
		{name: "unready AWS CPU ignored", mutate: func(n *corev1.Node) {
			n.Spec.ProviderID = "aws:///zone/node"
			n.Status.Conditions[0].Status = corev1.ConditionFalse
		}},
		{name: "deleting AWS CPU ignored", mutate: func(n *corev1.Node) {
			n.Spec.ProviderID = "aws:///zone/node"
			now := metav1.Now()
			n.DeletionTimestamp = &now
			n.Finalizers = []string{"test/finalizer"}
		}},
		{name: "instance filter excludes AWS CPU", instanceType: "Standard_D4s_v3", mutate: func(n *corev1.Node) {
			n.Spec.ProviderID = "aws:///zone/node"
			n.Labels[corev1.LabelInstanceTypeStable] = "m6i.2xlarge"
		}},
		{name: "matching CPU instance included", instanceType: "Standard_D4s_v3", mutate: func(n *corev1.Node) { n.Labels[corev1.LabelInstanceTypeStable] = "Standard_D4s_v3" }, wantCPU: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := cpuCreateValidationNode("node", false)
			tc.mutate(node)
			c := setupCPUCreateValidation(t, node)
			cpu, err := AreLabelSelectedReadyNodesCPU(context.Background(), cpuCreateValidationWorkspace().Resource.LabelSelector, tc.instanceType, c)
			if cpu != tc.wantCPU || (err != nil) != tc.wantError {
				t.Fatalf("CPU=%t error=%v, want CPU=%t error=%t", cpu, err, tc.wantCPU, tc.wantError)
			}
			if c.cpuLists != 1 || c.gpuLists != 0 {
				t.Fatalf("CPU/GPU Lists=%d/%d, want 1/0", c.cpuLists, c.gpuLists)
			}
		})
	}
}
