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

package multiroleinference

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func TestReconcileInferenceSetPropagatesAnnotations(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kaitov1alpha1.AddToScheme(scheme))
	require.NoError(t, kaitov1beta1.AddToScheme(scheme))

	mri := &kaitov1alpha1.MultiRoleInference{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mri-test",
			Namespace: "default",
			Annotations: map[string]string{
				"kaito.sh/model-streaming":   "disabled",
				"kaito.sh/disable-benchmark": "true",
			},
		},
		Spec: kaitov1alpha1.MultiRoleInferenceSpec{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "mri-test"}},
			Model:         kaitov1alpha1.MultiRoleInferenceModelSpec{Name: "gemma-3-4b-instruct"},
		},
	}
	role := kaitov1alpha1.MultiRoleInferenceRoleSpec{
		Type:         kaitov1alpha1.MultiRoleInferenceRolePrefill,
		InstanceType: "Standard_NV36ads_A10_v5",
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mri).Build()
	r := &MultiRoleInferenceReconciler{Client: cl, Scheme: scheme}

	require.NoError(t, r.reconcileInferenceSet(context.Background(), mri, role))

	got := &kaitov1beta1.InferenceSet{}
	require.NoError(t, cl.Get(context.Background(),
		client.ObjectKey{Name: "mri-test-prefill", Namespace: "default"}, got))

	assert.Equal(t, "disabled", got.Spec.Template.Annotations["kaito.sh/model-streaming"])
	assert.Equal(t, "true", got.Spec.Template.Annotations["kaito.sh/disable-benchmark"])
}
