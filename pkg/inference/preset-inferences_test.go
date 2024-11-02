// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package inference

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kaito-project/kaito/pkg/utils/consts"

	"github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/utils/test"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ValidStrength string = "0.5"

func TestCreatePresetInference(t *testing.T) {
	test.RegisterTestModel()
	testcases := map[string]struct {
		nodeCount      int
		modelName      string
		callMocks      func(c *test.MockClient)
		workload       string
		expectedCmd    string
		hasAdapters    bool
		expectedVolume string
	}{

		"test-model": {
			nodeCount: 1,
			modelName: "test-model",
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			workload: "Deployment",
			// No BaseCommand, TorchRunParams, TorchRunRdzvParams, or ModelRunParams
			// So expected cmd consists of shell command and inference file
			expectedCmd: "/bin/sh -c  inference_api.py",
			hasAdapters: false,
		},

		"test-distributed-model": {
			nodeCount: 1,
			modelName: "test-distributed-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)
			},
			workload:    "StatefulSet",
			expectedCmd: "/bin/sh -c  inference_api.py",
			hasAdapters: false,
		},

		"test-model-with-adapters": {
			nodeCount: 1,
			modelName: "test-model",
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			workload:       "Deployment",
			expectedCmd:    "/bin/sh -c  inference_api.py",
			hasAdapters:    true,
			expectedVolume: "adapter-volume",
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			os.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			workspace := test.MockWorkspaceWithPreset
			workspace.Resource.Count = &tc.nodeCount
			expectedSecrets := []string{"fake-secret"}
			if tc.hasAdapters {
				workspace.Inference.Adapters = []v1alpha1.AdapterSpec{
					{
						Source: &v1alpha1.DataSource{
							Name:             "Adapter-1",
							Image:            "fake.kaito.com/kaito-image:0.0.1",
							ImagePullSecrets: expectedSecrets,
						},
						Strength: &ValidStrength,
					},
				}
			}

			useHeadlessSvc := false

			var inferenceObj *model.PresetParam
			model := plugin.KaitoModelRegister.MustGet(tc.modelName)
			inferenceObj = model.GetInferenceParameters()

			if strings.Contains(tc.modelName, "distributed") {
				useHeadlessSvc = true
			}
			svc := &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Name:      workspace.Name,
					Namespace: workspace.Namespace,
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
				},
			}
			mockClient.CreateOrUpdateObjectInMap(svc)

			createdObject, _ := CreatePresetInference(context.TODO(), workspace, test.MockWorkspaceWithPresetHash, inferenceObj, useHeadlessSvc, mockClient)
			createdWorkload := ""
			switch createdObject.(type) {
			case *appsv1.Deployment:
				createdWorkload = "Deployment"
			case *appsv1.StatefulSet:
				createdWorkload = "StatefulSet"
			}
			if tc.workload != createdWorkload {
				t.Errorf("%s: returned worklaod type is wrong", k)
			}

			var workloadCmd string
			if tc.workload == "Deployment" {
				workloadCmd = strings.Join((createdObject.(*appsv1.Deployment)).Spec.Template.Spec.Containers[0].Command, " ")
			} else {
				workloadCmd = strings.Join((createdObject.(*appsv1.StatefulSet)).Spec.Template.Spec.Containers[0].Command, " ")
			}

			mainCmd := strings.Split(workloadCmd, "--")[0]
			params := toParameterMap(strings.Split(workloadCmd, "--")[1:])

			expectedMaincmd := strings.Split(tc.expectedCmd, "--")[0]
			expectedParams := toParameterMap(strings.Split(workloadCmd, "--")[1:])

			if mainCmd != expectedMaincmd {
				t.Errorf("%s main cmdline is not expected, got %s, expect %s ", k, workloadCmd, tc.expectedCmd)
			}

			if !reflect.DeepEqual(params, expectedParams) {
				t.Errorf("%s parameters are not expected, got %s, expect %s ", k, params, expectedParams)
			}

			// Check for adapter volume
			if tc.hasAdapters {
				var actualSecrets []string
				if tc.workload == "Deployment" {
					for _, secret := range createdObject.(*appsv1.Deployment).Spec.Template.Spec.ImagePullSecrets {
						actualSecrets = append(actualSecrets, secret.Name)
					}
				} else {
					for _, secret := range createdObject.(*appsv1.StatefulSet).Spec.Template.Spec.ImagePullSecrets {
						actualSecrets = append(actualSecrets, secret.Name)
					}
				}
				if !reflect.DeepEqual(expectedSecrets, actualSecrets) {
					t.Errorf("%s: ImagePullSecrets are not expected, got %v, expect %v", k, actualSecrets, expectedSecrets)
				}
				found := false
				for _, volume := range createdObject.(*appsv1.Deployment).Spec.Template.Spec.Volumes {
					if volume.Name == tc.expectedVolume {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: expected adapter volume %s not found", k, tc.expectedVolume)
				}
			}
		})
	}
}

func toParameterMap(in []string) map[string]string {
	ret := make(map[string]string)
	for _, each := range in {
		r := strings.Split(each, "=")
		k := r[0]
		var v string
		if len(r) == 1 {
			v = ""
		} else {
			v = r[1]
		}
		ret[k] = v
	}
	return ret
}
