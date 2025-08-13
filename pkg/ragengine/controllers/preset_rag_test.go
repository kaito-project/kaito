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
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestCreatePresetRAG(t *testing.T) {
	test.RegisterTestModel()

	testcases := map[string]struct {
		nodeCount            int
		registryNameOverride string
		imageNameOverride    string
		imageTagOverride     string
		callMocks            func(c *test.MockClient)
		expectedCmd          string
		expectedGPUReq       string
		expectedImage        string
		expectedVolume       string
	}{
		"test-rag-model": {
			nodeCount: 1,
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			expectedCmd:   "/bin/sh -c python3 main.py",
			expectedImage: "aimodelsregistrytest.azurecr.io/kaito-rag-service:0.3.2", //TODO: Change to the mcr image when release
		},
		"test-rag-model-with-image-from-env": {
			nodeCount:            1,
			registryNameOverride: "mcr.microsoft.com/aks/kaito",
			imageNameOverride:    "kaito-rag-engine",
			imageTagOverride:     "0.4.6",
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			expectedCmd:   "/bin/sh -c python3 main.py",
			expectedImage: "mcr.microsoft.com/aks/kaito/kaito-rag-engine:0.4.6",
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
			if tc.registryNameOverride != "" {
				t.Setenv("PRESET_RAG_REGISTRY_NAME", tc.registryNameOverride)
			}
			if tc.imageNameOverride != "" {
				t.Setenv("PRESET_RAG_IMAGE_NAME", tc.imageNameOverride)
			}
			if tc.imageTagOverride != "" {
				t.Setenv("PRESET_RAG_IMAGE_TAG", tc.imageTagOverride)
			}

			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			ragEngineObj := test.MockRAGEngineWithPreset
			createdObject, _ := CreatePresetRAG(context.TODO(), ragEngineObj, "1", mockClient)

			workloadCmd := strings.Join((createdObject.(*appsv1.Deployment)).Spec.Template.Spec.Containers[0].Command, " ")

			if workloadCmd != tc.expectedCmd {
				t.Errorf("%s: main cmdline is not expected, got %s, expected %s", k, workloadCmd, tc.expectedCmd)
			}

			image := (createdObject.(*appsv1.Deployment)).Spec.Template.Spec.Containers[0].Image

			if image != tc.expectedImage {
				t.Errorf("%s: image is not expected, got %s, expected %s", k, image, tc.expectedImage)
			}
		})
	}
}

func TestGPUConfigLogic(t *testing.T) {
	test.RegisterTestModel()

	testcases := map[string]struct {
		nodeCount        int
		callMocks        func(c *test.MockClient)
		ragEngine        *v1alpha1.RAGEngine
		expectedGPUReq   int64
		expectedLimitReq int64
	}{
		"test-rag-model": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			ragEngine:        test.MockRAGEngineWithPreset,
			expectedGPUReq:   int64(2),
			expectedLimitReq: int64(2),
		},
		"test-rag-preferred-nodes": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			ragEngine:        test.MockRAGEngineWithPresetPreferredCPUNodes,
			expectedGPUReq:   int64(0),
			expectedLimitReq: int64(0),
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			ragEngineObj := tc.ragEngine
			createdObject, _ := CreatePresetRAG(context.TODO(), ragEngineObj, "1", mockClient)

			resourceReq := createdObject.(*appsv1.Deployment).Spec.Template.Spec.Containers[0].Resources

			gpuRequests, exists := resourceReq.Requests[corev1.ResourceName(resources.CapacityNvidiaGPU)]
			if !exists {
				t.Errorf("GPU requests not found in resource requirements")
				return
			}

			gpuLimits, exists := resourceReq.Limits[corev1.ResourceName(resources.CapacityNvidiaGPU)]
			if !exists {
				t.Errorf("GPU limits not found in resource requirements")
				return
			}

			gpuRequestCount := gpuRequests.Value()
			gpuLimitCount := gpuLimits.Value()
			if gpuRequestCount != tc.expectedGPUReq {
				t.Errorf("%s: GPU request count is not expected, got %d, expected %d", k, gpuRequestCount, tc.expectedGPUReq)
			}
			if gpuLimitCount != tc.expectedLimitReq {
				t.Errorf("%s: GPU limit count is not expected, got %d, expected %d", k, gpuLimitCount, tc.expectedLimitReq)
			}
		})
	}
}
