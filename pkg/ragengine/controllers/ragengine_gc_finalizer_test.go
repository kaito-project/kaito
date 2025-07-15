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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestGarbageCollectRAGEngine(t *testing.T) {
	testcases := map[string]struct {
		callMocks      func(c *test.MockClient)
		ragengine      *kaitov1alpha1.RAGEngine
		expectedResult ctrl.Result
		expectedError  error
		verifyCalls    func(c *test.MockClient)
	}{
		"Successfully garbage collect RAGEngine with no nodeClaims": {
			callMocks: func(c *test.MockClient) {
				// Mock empty nodeClaim list
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				// Mock successful finalizer removal
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1alpha1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine: func() *kaitov1alpha1.RAGEngine {
				ragengine := test.MockRAGEngineDistributedModel.DeepCopy()
				ragengine.Finalizers = []string{consts.RAGEngineFinalizer}
				return ragengine
			}(),
			expectedResult: ctrl.Result{},
			expectedError:  nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},
		"Successfully garbage collect RAGEngine with existing nodeClaims": {
			callMocks: func(c *test.MockClient) {
				// Mock nodeClaim list with items
				nodeClaimList := &karpenterv1.NodeClaimList{
					Items: []karpenterv1.NodeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-nodeclaim-1",
								Namespace: "kaito",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-nodeclaim-2",
								Namespace: "kaito",
							},
						},
					},
				}

				relevantMap := c.CreateMapWithType(nodeClaimList)
				for _, obj := range nodeClaimList.Items {
					nc := obj
					objKey := client.ObjectKeyFromObject(&nc)
					relevantMap[objKey] = &nc
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil).Times(2)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1alpha1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine: func() *kaitov1alpha1.RAGEngine {
				ragengine := test.MockRAGEngineDistributedModel.DeepCopy()
				ragengine.Finalizers = []string{consts.RAGEngineFinalizer}
				return ragengine
			}(),
			expectedResult: ctrl.Result{},
			expectedError:  nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Delete", 2)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},
		"Successfully garbage collect RAGEngine with nodeClaims already being deleted": {
			callMocks: func(c *test.MockClient) {
				// Mock nodeClaim list with items that have deletion timestamp
				now := metav1.NewTime(time.Now())
				nodeClaimList := &karpenterv1.NodeClaimList{
					Items: []karpenterv1.NodeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:              "test-nodeclaim-1",
								Namespace:         "kaito",
								DeletionTimestamp: &now,
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:              "test-nodeclaim-2",
								Namespace:         "kaito",
								DeletionTimestamp: &now,
							},
						},
					},
				}

				relevantMap := c.CreateMapWithType(nodeClaimList)
				for _, obj := range nodeClaimList.Items {
					nc := obj
					objKey := client.ObjectKeyFromObject(&nc)
					relevantMap[objKey] = &nc
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				// No Delete calls should be made since nodeClaims are already being deleted
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1alpha1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine: func() *kaitov1alpha1.RAGEngine {
				ragengine := test.MockRAGEngineDistributedModel.DeepCopy()
				ragengine.Finalizers = []string{consts.RAGEngineFinalizer}
				return ragengine
			}(),
			expectedResult: ctrl.Result{},
			expectedError:  nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},
		"Fail to list nodeClaims": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("failed to list nodeClaims"))
			},
			ragengine: func() *kaitov1alpha1.RAGEngine {
				ragengine := test.MockRAGEngineDistributedModel.DeepCopy()
				ragengine.Finalizers = []string{consts.RAGEngineFinalizer}
				return ragengine
			}(),
			expectedResult: ctrl.Result{},
			expectedError:  errors.New("failed to list nodeClaims"),
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", retry.DefaultBackoff.Steps)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},
		"Fail to delete nodeClaim": {
			callMocks: func(c *test.MockClient) {
				// Mock nodeClaim list with items
				nodeClaimList := &karpenterv1.NodeClaimList{
					Items: []karpenterv1.NodeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-nodeclaim-1",
								Namespace: "kaito",
							},
						},
					},
				}

				relevantMap := c.CreateMapWithType(nodeClaimList)
				for _, obj := range nodeClaimList.Items {
					nc := obj
					objKey := client.ObjectKeyFromObject(&nc)
					relevantMap[objKey] = &nc
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("failed to delete nodeClaim"))
			},
			ragengine: func() *kaitov1alpha1.RAGEngine {
				ragengine := test.MockRAGEngineDistributedModel.DeepCopy()
				ragengine.Finalizers = []string{consts.RAGEngineFinalizer}
				return ragengine
			}(),
			expectedResult: ctrl.Result{},
			expectedError:  errors.New("failed to delete nodeClaim"),
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Delete", 1)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},
		"Fail to remove finalizer": {
			callMocks: func(c *test.MockClient) {
				// Mock empty nodeClaim list
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				// Mock failed finalizer removal
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1alpha1.RAGEngine{}), mock.Anything).Return(errors.New("failed to update RAGEngine"))
			},
			ragengine: func() *kaitov1alpha1.RAGEngine {
				ragengine := test.MockRAGEngineDistributedModel.DeepCopy()
				ragengine.Finalizers = []string{consts.RAGEngineFinalizer}
				return ragengine
			}(),
			expectedResult: ctrl.Result{},
			expectedError:  errors.New("failed to update RAGEngine"),
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},
		"RAGEngine with no finalizer to remove": {
			callMocks: func(c *test.MockClient) {
				// Mock empty nodeClaim list
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				// No Update call should be made since there's no finalizer to remove
			},
			ragengine: func() *kaitov1alpha1.RAGEngine {
				ragengine := test.MockRAGEngineDistributedModel.DeepCopy()
				ragengine.Finalizers = []string{} // No finalizers
				return ragengine
			}(),
			expectedResult: ctrl.Result{},
			expectedError:  nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},
		"RAGEngine with other finalizers but not RAGEngine finalizer": {
			callMocks: func(c *test.MockClient) {
				// Mock empty nodeClaim list
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				// No Update call should be made since RAGEngine finalizer is not present
			},
			ragengine: func() *kaitov1alpha1.RAGEngine {
				ragengine := test.MockRAGEngineDistributedModel.DeepCopy()
				ragengine.Finalizers = []string{"some.other/finalizer"}
				return ragengine
			}(),
			expectedResult: ctrl.Result{},
			expectedError:  nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},
		"Mixed scenario with some nodeClaims being deleted and some not": {
			callMocks: func(c *test.MockClient) {
				now := metav1.NewTime(time.Now())
				nodeClaimList := &karpenterv1.NodeClaimList{
					Items: []karpenterv1.NodeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-nodeclaim-1",
								Namespace: "kaito",
								// No DeletionTimestamp - not being deleted
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:              "test-nodeclaim-2",
								Namespace:         "kaito",
								DeletionTimestamp: &now, // Already being deleted
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-nodeclaim-3",
								Namespace: "kaito",
								// No DeletionTimestamp - not being deleted
							},
						},
					},
				}

				relevantMap := c.CreateMapWithType(nodeClaimList)
				for _, obj := range nodeClaimList.Items {
					nc := obj
					objKey := client.ObjectKeyFromObject(&nc)
					relevantMap[objKey] = &nc
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				// Only 2 Delete calls should be made (for nodeclaim-1 and nodeclaim-3)
				c.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil).Times(2)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1alpha1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine: func() *kaitov1alpha1.RAGEngine {
				ragengine := test.MockRAGEngineDistributedModel.DeepCopy()
				ragengine.Finalizers = []string{consts.RAGEngineFinalizer}
				return ragengine
			}(),
			expectedResult: ctrl.Result{},
			expectedError:  nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Delete", 2)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			ctx := context.Background()

			result, err := reconciler.garbageCollectRAGEngine(ctx, tc.ragengine)

			if tc.expectedError == nil {
				assert.NoError(t, err, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
			assert.Equal(t, tc.expectedResult, result)

			if tc.verifyCalls != nil {
				tc.verifyCalls(mockClient)
			}
		})
	}
}
