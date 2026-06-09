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

package autoupgrade

import (
	"context"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
)

// testScheme returns a scheme with all types needed for fake client tests.
func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = kaitov1alpha1.AddToScheme(s)
	_ = kaitov1beta1.AddToScheme(s)
	return s
}

// newFakeClient creates a fake client with the test scheme and the given objects.
func newFakeClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(objs...).
		WithStatusSubresource(&kaitov1alpha1.InferenceSet{}).
		Build()
}

// setTestRegistry configures PRESET_REGISTRY_NAME and returns the base image
// that inference.GetBaseImageName() will produce during the test.
func setTestRegistry(t *testing.T) string {
	t.Helper()
	t.Setenv("PRESET_REGISTRY_NAME", "mcr.microsoft.com/aks/kaito")
	return inference.GetBaseImageName()
}

func makeInferenceSet(name, namespace string, enabled bool, window *kaitov1alpha1.MaintenanceWindow) *kaitov1alpha1.InferenceSet {
	is := &kaitov1alpha1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: kaitov1alpha1.InferenceSetSpec{},
	}
	if enabled {
		is.Spec.AutoUpgrade = &kaitov1alpha1.AutoUpgradePolicy{
			Enabled:           true,
			MaintenanceWindow: window,
		}
	}
	return is
}

func makeWorkspace(name, namespace, inferenceSetName string, state kaitov1beta1.WorkspaceState, labels map[string]string) *kaitov1beta1.Workspace {
	if labels == nil {
		labels = map[string]string{}
	}
	labels[consts.WorkspaceCreatedByInferenceSetLabel] = inferenceSetName
	return &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Status: kaitov1beta1.WorkspaceStatus{
			State: state,
		},
	}
}

func makeStatefulSet(name, namespace, image string) *appsv1.StatefulSet {
	one := int32(1)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &one,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "inference",
						Image: image,
					}},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas:      1,
			ObservedGeneration: 1,
		},
	}
}

func TestIsWithinWindow(t *testing.T) {
	// Schedule: every Saturday at 02:00 UTC
	schedule, err := cron.ParseStandard("0 2 * * 6")
	assert.NoError(t, err)
	duration := 4 * time.Hour

	tests := []struct {
		name   string
		now    time.Time
		expect bool
	}{
		{
			name:   "within window - just after open",
			now:    time.Date(2026, 5, 16, 2, 30, 0, 0, time.UTC), // Saturday 02:30
			expect: true,
		},
		{
			name:   "within window - near end",
			now:    time.Date(2026, 5, 16, 5, 59, 0, 0, time.UTC), // Saturday 05:59
			expect: true,
		},
		{
			name:   "at window open",
			now:    time.Date(2026, 5, 16, 2, 0, 0, 0, time.UTC), // Saturday 02:00
			expect: true,
		},
		{
			name:   "outside window - after close",
			now:    time.Date(2026, 5, 16, 6, 1, 0, 0, time.UTC), // Saturday 06:01
			expect: false,
		},
		{
			name:   "outside window - different day",
			now:    time.Date(2026, 5, 17, 3, 0, 0, 0, time.UTC), // Sunday 03:00
			expect: false,
		},
		{
			name:   "outside window - before open on same day",
			now:    time.Date(2026, 5, 16, 1, 59, 0, 0, time.UTC), // Saturday 01:59
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWithinWindow(schedule, duration, tt.now)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestIsWorkspaceUpgradeComplete(t *testing.T) {
	desiredImage := "mcr.microsoft.com/aks/kaito/kaito-base:0.4.0"
	one := int32(1)

	tests := []struct {
		name   string
		ss     *appsv1.StatefulSet
		expect bool
	}{
		{
			name: "complete - image matches, replicas ready, generation observed",
			ss: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &one,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Image: desiredImage}},
						},
					},
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas:      1,
					ObservedGeneration: 2,
				},
			},
			expect: true,
		},
		{
			name: "not complete - image matches but replicas not ready",
			ss: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &one,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Image: desiredImage}},
						},
					},
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas:      0,
					ObservedGeneration: 2,
				},
			},
			expect: false,
		},
		{
			name: "not complete - image matches, replicas ready, but generation not observed (rollout pending)",
			ss: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 3},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &one,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Image: desiredImage}},
						},
					},
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas:      1,
					ObservedGeneration: 2,
				},
			},
			expect: false,
		},
		{
			name: "not complete - image differs",
			ss: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &one,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Image: "mcr.microsoft.com/aks/kaito/kaito-base:0.3.0"}},
						},
					},
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas:      1,
					ObservedGeneration: 1,
				},
			},
			expect: false,
		},
		{
			name: "not complete - no containers",
			ss: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Spec: appsv1.StatefulSetSpec{
					Replicas: &one,
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{},
						},
					},
				},
				Status: appsv1.StatefulSetStatus{
					ReadyReplicas:      1,
					ObservedGeneration: 1,
				},
			},
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWorkspaceInDesiredState(tt.ss, desiredImage)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestStatusNeedsUpdate(t *testing.T) {
	r := &AutoUpgradeRunner{}
	driftCount := 3

	tests := []struct {
		name          string
		isObj         *kaitov1alpha1.InferenceSet
		newDriftCount int
		expect        bool
	}{
		{
			name: "nil AutoUpgrade status and non-zero drift",
			isObj: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{},
			},
			newDriftCount: 2,
			expect:        true,
		},
		{
			name: "nil AutoUpgrade status and zero drift",
			isObj: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{},
			},
			newDriftCount: 0,
			expect:        true,
		},
		{
			name: "nil NumDriftedWorkspaces and non-zero drift",
			isObj: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{
					AutoUpgrade: &kaitov1alpha1.AutoUpgradeStatus{},
				},
			},
			newDriftCount: 1,
			expect:        true,
		},
		{
			name: "same drift count - no update needed",
			isObj: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{
					AutoUpgrade: &kaitov1alpha1.AutoUpgradeStatus{
						NumDriftedWorkspaces: &driftCount,
					},
				},
			},
			newDriftCount: 3,
			expect:        false,
		},
		{
			name: "different drift count - update needed",
			isObj: &kaitov1alpha1.InferenceSet{
				Status: kaitov1alpha1.InferenceSetStatus{
					AutoUpgrade: &kaitov1alpha1.AutoUpgradeStatus{
						NumDriftedWorkspaces: &driftCount,
					},
				},
			},
			newDriftCount: 1,
			expect:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.statusNeedsUpdate(tt.isObj, tt.newDriftCount)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestNeedLeaderElection(t *testing.T) {
	r := &AutoUpgradeRunner{}
	assert.True(t, r.NeedLeaderElection())
}

func TestCategorizeWorkspaces(t *testing.T) {
	const (
		ns           = "default"
		isName       = "test-is"
		desiredImage = "mcr.microsoft.com/aks/kaito/kaito-base:0.4.0"
		desiredTag   = "0.4.0"
		oldImage     = "mcr.microsoft.com/aks/kaito/kaito-base:0.3.0"
	)

	tests := []struct {
		name            string
		workspaces      []*kaitov1beta1.Workspace
		statefulSets    []*appsv1.StatefulSet
		expectToUpgrade int
		expectUpgrading int
		expectErr       bool
	}{
		{
			name: "all workspaces already at desired state",
			workspaces: []*kaitov1beta1.Workspace{
				makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, nil),
				makeWorkspace("ws-2", ns, isName, kaitov1beta1.WorkspaceStateReady, nil),
			},
			statefulSets: []*appsv1.StatefulSet{
				makeStatefulSet("ws-1", ns, desiredImage),
				makeStatefulSet("ws-2", ns, desiredImage),
			},
			expectToUpgrade: 0,
			expectUpgrading: 0,
		},
		{
			name: "one workspace drifted, one at desired state",
			workspaces: []*kaitov1beta1.Workspace{
				makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, nil),
				makeWorkspace("ws-2", ns, isName, kaitov1beta1.WorkspaceStateReady, nil),
			},
			statefulSets: []*appsv1.StatefulSet{
				makeStatefulSet("ws-1", ns, desiredImage),
				makeStatefulSet("ws-2", ns, oldImage),
			},
			expectToUpgrade: 1,
			expectUpgrading: 0,
		},
		{
			name: "workspace with upgrade label for current version is upgrading",
			workspaces: []*kaitov1beta1.Workspace{
				makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, map[string]string{
					kaitov1alpha1.LabelUpgradeToVersion: desiredTag,
				}),
			},
			statefulSets: []*appsv1.StatefulSet{
				makeStatefulSet("ws-1", ns, oldImage), // not yet updated
			},
			expectToUpgrade: 0,
			expectUpgrading: 1,
		},
		{
			name: "workspace with stale upgrade label is toUpgrade",
			workspaces: []*kaitov1beta1.Workspace{
				makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, map[string]string{
					kaitov1alpha1.LabelUpgradeToVersion: "0.3.0", // stale label
				}),
			},
			statefulSets: []*appsv1.StatefulSet{
				makeStatefulSet("ws-1", ns, oldImage),
			},
			expectToUpgrade: 1,
			expectUpgrading: 0,
		},
		{
			name: "deleting workspaces are skipped",
			workspaces: []*kaitov1beta1.Workspace{
				func() *kaitov1beta1.Workspace {
					ws := makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, nil)
					now := metav1.Now()
					ws.DeletionTimestamp = &now
					ws.Finalizers = []string{"test-finalizer"}
					return ws
				}(),
			},
			statefulSets: []*appsv1.StatefulSet{
				makeStatefulSet("ws-1", ns, oldImage),
			},
			expectToUpgrade: 0,
			expectUpgrading: 0,
		},
		{
			name: "workspace ready with desired image and upgrade label is complete",
			workspaces: []*kaitov1beta1.Workspace{
				makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, map[string]string{
					kaitov1alpha1.LabelUpgradeToVersion: desiredTag,
				}),
			},
			statefulSets: []*appsv1.StatefulSet{
				makeStatefulSet("ws-1", ns, desiredImage),
			},
			expectToUpgrade: 0,
			expectUpgrading: 0,
		},
		{
			name: "missing StatefulSet returns error",
			workspaces: []*kaitov1beta1.Workspace{
				makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, nil),
			},
			statefulSets: []*appsv1.StatefulSet{}, // no statefulset
			expectErr:    true,
		},
		{
			name: "mixed states - complete, upgrading, and toUpgrade",
			workspaces: []*kaitov1beta1.Workspace{
				makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, nil),
				makeWorkspace("ws-2", ns, isName, kaitov1beta1.WorkspaceStateReady, map[string]string{
					kaitov1alpha1.LabelUpgradeToVersion: desiredTag,
				}),
				makeWorkspace("ws-3", ns, isName, kaitov1beta1.WorkspaceStateReady, nil),
			},
			statefulSets: []*appsv1.StatefulSet{
				makeStatefulSet("ws-1", ns, desiredImage), // complete
				makeStatefulSet("ws-2", ns, oldImage),     // upgrading (has label, not yet done)
				makeStatefulSet("ws-3", ns, oldImage),     // toUpgrade (no label)
			},
			expectToUpgrade: 1,
			expectUpgrading: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []client.Object
			for _, ss := range tt.statefulSets {
				objs = append(objs, ss)
			}
			cl := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(objs...).Build()
			r := &AutoUpgradeRunner{Client: cl}

			var wsList []kaitov1beta1.Workspace
			for _, ws := range tt.workspaces {
				wsList = append(wsList, *ws)
			}

			toUpgrade, upgrading, err := r.categorizeWorkspaces(context.Background(), wsList, desiredImage, desiredTag)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, toUpgrade, tt.expectToUpgrade)
			assert.Len(t, upgrading, tt.expectUpgrading)
		})
	}
}

func TestTagWorkspaceForUpgrade(t *testing.T) {
	const (
		ns         = "default"
		isName     = "test-is"
		desiredTag = "0.4.0"
	)

	ws := makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, nil)
	is := makeInferenceSet(isName, ns, true, nil)
	cl := newFakeClient(ws, is)
	r := &AutoUpgradeRunner{Client: cl}

	r.tagWorkspaceForUpgrade(context.Background(), is, ws, desiredTag)

	// Verify the label was set.
	updated := &kaitov1beta1.Workspace{}
	err := cl.Get(context.Background(), client.ObjectKeyFromObject(ws), updated)
	require.NoError(t, err)
	assert.Equal(t, desiredTag, updated.Labels[kaitov1alpha1.LabelUpgradeToVersion])

	// Verify the annotation was set.
	startTime, ok := updated.Annotations[AnnotationUpgradeStartTime]
	assert.True(t, ok)
	_, err = time.Parse(time.RFC3339, startTime)
	assert.NoError(t, err, "upgrade start time should be valid RFC3339")
}

func TestUpdateStatus(t *testing.T) {
	const (
		ns     = "default"
		isName = "test-is"
	)

	t.Run("sets drift count", func(t *testing.T) {
		is := makeInferenceSet(isName, ns, true, nil)
		cl := newFakeClient(is)
		r := &AutoUpgradeRunner{Client: cl}

		err := r.updateStatus(context.Background(), is, 3, false)
		require.NoError(t, err)

		updated := &kaitov1alpha1.InferenceSet{}
		err = cl.Get(context.Background(), client.ObjectKeyFromObject(is), updated)
		require.NoError(t, err)
		require.NotNil(t, updated.Status.AutoUpgrade)
		require.NotNil(t, updated.Status.AutoUpgrade.NumDriftedWorkspaces)
		assert.Equal(t, 3, *updated.Status.AutoUpgrade.NumDriftedWorkspaces)
		assert.Nil(t, updated.Status.AutoUpgrade.LastSuccessfulUpgradeTime)
	})

	t.Run("sets lastSuccessfulUpgradeTime when markSuccess is true", func(t *testing.T) {
		is := makeInferenceSet(isName, ns, true, nil)
		cl := newFakeClient(is)
		r := &AutoUpgradeRunner{Client: cl}

		err := r.updateStatus(context.Background(), is, 0, true)
		require.NoError(t, err)

		updated := &kaitov1alpha1.InferenceSet{}
		err = cl.Get(context.Background(), client.ObjectKeyFromObject(is), updated)
		require.NoError(t, err)
		require.NotNil(t, updated.Status.AutoUpgrade)
		assert.Equal(t, 0, *updated.Status.AutoUpgrade.NumDriftedWorkspaces)
		assert.NotNil(t, updated.Status.AutoUpgrade.LastSuccessfulUpgradeTime)
	})

	t.Run("does not set lastSuccessfulUpgradeTime when markSuccess is false", func(t *testing.T) {
		is := makeInferenceSet(isName, ns, true, nil)
		cl := newFakeClient(is)
		r := &AutoUpgradeRunner{Client: cl}

		err := r.updateStatus(context.Background(), is, 2, false)
		require.NoError(t, err)

		updated := &kaitov1alpha1.InferenceSet{}
		err = cl.Get(context.Background(), client.ObjectKeyFromObject(is), updated)
		require.NoError(t, err)
		assert.Nil(t, updated.Status.AutoUpgrade.LastSuccessfulUpgradeTime)
	})
}

func TestIsWithinMaintenanceWindow(t *testing.T) {
	r := &AutoUpgradeRunner{}

	t.Run("no window configured - always returns true", func(t *testing.T) {
		is := makeInferenceSet("test", "default", true, nil)
		assert.True(t, r.isWithinMaintenanceWindow(is))
	})

	t.Run("autoUpgrade nil - returns true", func(t *testing.T) {
		is := &kaitov1alpha1.InferenceSet{
			Spec: kaitov1alpha1.InferenceSetSpec{},
		}
		assert.True(t, r.isWithinMaintenanceWindow(is))
	})

	t.Run("invalid cron schedule - returns false", func(t *testing.T) {
		is := makeInferenceSet("test", "default", true, &kaitov1alpha1.MaintenanceWindow{
			Schedule: "invalid cron",
		})
		assert.False(t, r.isWithinMaintenanceWindow(is))
	})
}

func TestReconcileInferenceSet_AutoUpgradeDisabled(t *testing.T) {
	desiredImage := setTestRegistry(t)
	const (
		ns     = "default"
		isName = "test-is"
	)

	is := makeInferenceSet(isName, ns, false, nil) // disabled
	ws := makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, nil)
	ss := makeStatefulSet("ws-1", ns, desiredImage)
	cl := newFakeClient(is, ws, ss)
	r := &AutoUpgradeRunner{Client: cl}

	r.reconcileInferenceSet(context.Background(), is)

	// Verify no label was added.
	updated := &kaitov1beta1.Workspace{}
	_ = cl.Get(context.Background(), client.ObjectKeyFromObject(ws), updated)
	assert.Empty(t, updated.Labels[kaitov1alpha1.LabelUpgradeToVersion])
}

func TestReconcileInferenceSet_AllUpToDate(t *testing.T) {
	desiredImage := setTestRegistry(t)
	const (
		ns     = "default"
		isName = "test-is"
	)

	is := makeInferenceSet(isName, ns, true, nil)
	ws := makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, nil)
	// StatefulSet already at desired image.
	ss := makeStatefulSet("ws-1", ns, desiredImage)
	cl := newFakeClient(is, ws, ss)
	r := &AutoUpgradeRunner{Client: cl}

	r.reconcileInferenceSet(context.Background(), is)

	// NumDriftedWorkspaces should always be reported, even when 0.
	updated := &kaitov1alpha1.InferenceSet{}
	_ = cl.Get(context.Background(), client.ObjectKeyFromObject(is), updated)
	require.NotNil(t, updated.Status.AutoUpgrade)
	require.NotNil(t, updated.Status.AutoUpgrade.NumDriftedWorkspaces)
	assert.Equal(t, 0, *updated.Status.AutoUpgrade.NumDriftedWorkspaces)
}

func TestReconcileInferenceSet_WaitsForUpgrading(t *testing.T) {
	setTestRegistry(t)
	desiredTag := inference.GetBaseImageTag()
	const (
		ns     = "default"
		isName = "test-is"
	)

	is := makeInferenceSet(isName, ns, true, nil)
	// ws-1 is upgrading (has label for current desired version, not yet at desired state).
	ws1 := makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, map[string]string{
		kaitov1alpha1.LabelUpgradeToVersion: desiredTag,
	})
	ws1.Annotations = map[string]string{
		AnnotationUpgradeStartTime: time.Now().UTC().Format(time.RFC3339),
	}
	// ws-2 is drifted.
	ws2 := makeWorkspace("ws-2", ns, isName, kaitov1beta1.WorkspaceStateReady, nil)

	ss1 := makeStatefulSet("ws-1", ns, "mcr.microsoft.com/aks/kaito/kaito-base:0.2.0") // not yet updated
	ss2 := makeStatefulSet("ws-2", ns, "mcr.microsoft.com/aks/kaito/kaito-base:0.2.0")
	cl := newFakeClient(is, ws1, ws2, ss1, ss2)
	r := &AutoUpgradeRunner{Client: cl}

	r.reconcileInferenceSet(context.Background(), is)

	// ws-2 should NOT be tagged because ws-1 is still upgrading.
	updated := &kaitov1beta1.Workspace{}
	_ = cl.Get(context.Background(), client.ObjectKeyFromObject(ws2), updated)
	assert.Empty(t, updated.Labels[kaitov1alpha1.LabelUpgradeToVersion])
}

func TestReconcileInferenceSet_MarkSuccessOnDriftTransition(t *testing.T) {
	desiredImage := setTestRegistry(t)
	desiredTag := inference.GetBaseImageTag()
	const (
		ns     = "default"
		isName = "test-is"
	)

	prevDrift := 1
	is := makeInferenceSet(isName, ns, true, nil)
	is.Status.AutoUpgrade = &kaitov1alpha1.AutoUpgradeStatus{
		NumDriftedWorkspaces: &prevDrift,
	}
	// All workspaces now at desired state.
	ws := makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, map[string]string{
		kaitov1alpha1.LabelUpgradeToVersion: desiredTag,
	})
	ss := makeStatefulSet("ws-1", ns, desiredImage)
	cl := newFakeClient(is, ws, ss)
	r := &AutoUpgradeRunner{Client: cl}

	r.reconcileInferenceSet(context.Background(), is)

	// Should set lastSuccessfulUpgradeTime.
	updated := &kaitov1alpha1.InferenceSet{}
	_ = cl.Get(context.Background(), client.ObjectKeyFromObject(is), updated)
	require.NotNil(t, updated.Status.AutoUpgrade)
	assert.Equal(t, 0, *updated.Status.AutoUpgrade.NumDriftedWorkspaces)
	assert.NotNil(t, updated.Status.AutoUpgrade.LastSuccessfulUpgradeTime)
}

func TestReconcileInferenceSet_NoSuccessWhenPreviousDriftWasZero(t *testing.T) {
	desiredImage := setTestRegistry(t)
	const (
		ns     = "default"
		isName = "test-is"
	)

	// Previous drift was nil (fresh InferenceSet), workspaces already at desired.
	is := makeInferenceSet(isName, ns, true, nil)
	ws := makeWorkspace("ws-1", ns, isName, kaitov1beta1.WorkspaceStateReady, nil)
	ss := makeStatefulSet("ws-1", ns, desiredImage)
	cl := newFakeClient(is, ws, ss)
	r := &AutoUpgradeRunner{Client: cl}

	r.reconcileInferenceSet(context.Background(), is)

	// NumDriftedWorkspaces is reported as 0, but no lastSuccessfulUpgradeTime
	// since there was no drift transition (previous was nil/0).
	updated := &kaitov1alpha1.InferenceSet{}
	_ = cl.Get(context.Background(), client.ObjectKeyFromObject(is), updated)
	require.NotNil(t, updated.Status.AutoUpgrade)
	assert.Equal(t, 0, *updated.Status.AutoUpgrade.NumDriftedWorkspaces)
	assert.Nil(t, updated.Status.AutoUpgrade.LastSuccessfulUpgradeTime)
}
