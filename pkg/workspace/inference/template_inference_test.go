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

package inference

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/cache"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestCreateTemplateInference(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
	}{
		"Fail to create template inference because deployment creation fails": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(test.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(errors.New("Failed to create resource"))
			},
			expectedError: errors.New("Failed to create resource"),
		},
		"Successfully returns existing template inference": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
		"Successfully creates template inference by creating a new deployment": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(test.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			obj, err := CreateTemplateInference(context.Background(), test.MockWorkspaceWithInferenceTemplate, mockClient, nil)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
				assert.Check(t, obj != nil, "Return object should not be nil")

				statefulSetObj, ok := obj.(*appsv1.StatefulSet)
				assert.Check(t, ok, "Returned object should be of type *v1.StatefulSet")
				assert.Check(t, statefulSetObj != nil, "Returned object should not be nil")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

type templateCacheProvider struct{}

func (p *templateCacheProvider) Name() string { return "template-reconcile-test" }
func (p *templateCacheProvider) IsAvailable(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (p *templateCacheProvider) IsReady(_ context.Context, _ string) (bool, string, error) {
	return true, "ready", nil
}
func (p *templateCacheProvider) ValidateParameters(_ cache.CacheConcern, parameters map[string]string) error {
	for name := range parameters {
		if name != "value" {
			return errors.New("unexpected parameter")
		}
	}
	return nil
}
func (p *templateCacheProvider) PodMutations(_ context.Context, _ cache.CacheConcern,
	_ *kaitov1beta1.Workspace, _, _ string, parameters map[string]string) (*cache.PodMutations, error) {
	return &cache.PodMutations{
		EnvVars: []corev1.EnvVar{{Name: "CACHE_VALUE", Value: parameters["value"]}},
	}, nil
}

func TestCreateTemplateInference_ReconcilesCacheMutationChanges(t *testing.T) {
	oldGate := featuregates.FeatureGates[consts.FeatureFlagDistributedCache]
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = oldGate })
	cache.Register(&templateCacheProvider{})

	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cache-config", Namespace: "kaito"},
		Data: map[string]string{cache.ConfigKey: `modelWeights:
  provider: template-reconcile-test
  mode: Required
  parameters:
    value: one
`},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()
	ws := test.MockWorkspaceWithInferenceTemplate.DeepCopy()
	ws.Inference.Template.Spec.Containers = []corev1.Container{{
		Name:  "model",
		Image: "registry.example/kaito-base:v1",
	}}

	if _, err := CreateTemplateInference(context.Background(), ws, kubeClient, nil); err != nil {
		t.Fatalf("create template inference: %v", err)
	}

	ws.Annotations = map[string]string{cache.ConfigAnnotation: configMap.Name}
	if _, err := CreateTemplateInference(context.Background(), ws, kubeClient, nil); err != nil {
		t.Fatalf("add cache mutations: %v", err)
	}
	assertTemplateCacheValue(t, kubeClient, ws, "one", true)

	currentConfig := &corev1.ConfigMap{}
	if err := kubeClient.Get(context.Background(), client.ObjectKeyFromObject(configMap), currentConfig); err != nil {
		t.Fatal(err)
	}
	currentConfig.Data[cache.ConfigKey] = `modelWeights:
  provider: template-reconcile-test
  mode: Required
  parameters:
    value: two
`
	if err := kubeClient.Update(context.Background(), currentConfig); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateTemplateInference(context.Background(), ws, kubeClient, nil); err != nil {
		t.Fatalf("change cache mutations: %v", err)
	}
	assertTemplateCacheValue(t, kubeClient, ws, "two", true)

	delete(ws.Annotations, cache.ConfigAnnotation)
	if _, err := CreateTemplateInference(context.Background(), ws, kubeClient, nil); err != nil {
		t.Fatalf("remove cache mutations: %v", err)
	}
	assertTemplateCacheValue(t, kubeClient, ws, "", false)
}

func assertTemplateCacheValue(t *testing.T, kubeClient client.Client, ws *kaitov1beta1.Workspace,
	wantValue string, wantHash bool) {
	t.Helper()
	ss := &appsv1.StatefulSet{}
	if err := kubeClient.Get(context.Background(), client.ObjectKey{Name: ws.Name, Namespace: ws.Namespace}, ss); err != nil {
		t.Fatal(err)
	}
	_, hasHash := ss.Spec.Template.Annotations[cache.MutationHashAnnotation]
	assert.Equal(t, wantHash, hasHash)

	var gotValue string
	for _, env := range ss.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "CACHE_VALUE" {
			gotValue = env.Value
		}
	}
	assert.Equal(t, wantValue, gotValue)
}
