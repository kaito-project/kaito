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
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/utils/test"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

var ValidStrength string = "0.5"

func TestGeneratePresetInference(t *testing.T) {
	test.RegisterTestModel()
	baseImage := metadata.MustGet("base")
	baseImageName := fmt.Sprintf("test-registry/kaito-base:%s", baseImage.Tag)
	testcases := map[string]struct {
		workspace          *v1beta1.Workspace
		nodeCount          int
		modelName          string
		callMocks          func(c *test.MockClient)
		workload           string
		expectedCmd        string
		hasAdapters        bool
		expectedModelImage string
		expectedVolume     string
		expectedEnvVars    []corev1.EnvVar
	}{
		"test-model/vllm": {
			workspace: test.MockWorkspaceWithPresetVLLM,
			nodeCount: 1,
			modelName: "test-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
			},
			workload:           "Deployment",
			expectedModelImage: "test-registry/kaito-test-model:1.0.0",
			// No BaseCommand, AccelerateParams, or ModelRunParams
			// So expected cmd consists of shell command and inference file
			expectedCmd: "/bin/sh -c python3 /workspace/vllm/inference_api.py --tensor-parallel-size=2 --served-model-name=mymodel --gpu-memory-utilization=0.90 --kaito-config-file=/mnt/config/inference_config.yaml",
			hasAdapters: false,
		},

		"test-model-no-parallel/vllm": {
			workspace: test.MockWorkspaceWithPresetVLLM,
			nodeCount: 1,
			modelName: "test-no-tensor-parallel-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
			},
			workload:           "Deployment",
			expectedModelImage: "test-registry/kaito-test-no-tensor-parallel-model:1.0.0",
			// No BaseCommand, AccelerateParams, or ModelRunParams
			// So expected cmd consists of shell command and inference file
			expectedCmd: "/bin/sh -c python3 /workspace/vllm/inference_api.py --kaito-config-file=/mnt/config/inference_config.yaml --gpu-memory-utilization=0.90",
			hasAdapters: false,
		},

		"test-model-no-lora-support/vllm": {
			workspace: test.MockWorkspaceWithPresetVLLM,
			nodeCount: 1,
			modelName: "test-no-lora-support-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
			},
			workload:           "Deployment",
			expectedModelImage: "test-registry/kaito-test-no-lora-support-model:1.0.0",
			// No BaseCommand, AccelerateParams, or ModelRunParams
			// So expected cmd consists of shell command and inference file
			expectedCmd: "/bin/sh -c python3 /workspace/vllm/inference_api.py --kaito-config-file=/mnt/config/inference_config.yaml --gpu-memory-utilization=0.90",
			hasAdapters: false,
		},

		"test-model-with-adapters/vllm": {
			workspace: test.MockWorkspaceWithPresetVLLM,
			nodeCount: 1,
			modelName: "test-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
			},
			workload:           "Deployment",
			expectedModelImage: "test-registry/kaito-test-model:1.0.0",
			expectedCmd:        "/bin/sh -c python3 /workspace/vllm/inference_api.py --enable-lora --tensor-parallel-size=2 --served-model-name=mymodel --gpu-memory-utilization=0.90 --kaito-config-file=/mnt/config/inference_config.yaml",
			hasAdapters:        true,
			expectedVolume:     "adapter-volume",
			expectedEnvVars: []corev1.EnvVar{{
				Name:  "Adapter-1",
				Value: "0.5",
			}},
		},

		"test-model/transformers": {
			workspace: test.MockWorkspaceWithPreset,
			nodeCount: 1,
			modelName: "test-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
			},
			workload:           "Deployment",
			expectedModelImage: "test-registry/kaito-test-model:1.0.0",
			// No BaseCommand, AccelerateParams, or ModelRunParams
			// So expected cmd consists of shell command and inference file
			expectedCmd: "/bin/sh -c accelerate launch /workspace/tfs/inference_api.py",
			hasAdapters: false,
		},

		"test-model-with-adapters": {
			workspace: test.MockWorkspaceWithPreset,
			nodeCount: 1,
			modelName: "test-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
			},
			workload:           "Deployment",
			expectedModelImage: "test-registry/kaito-test-model:1.0.0",
			expectedCmd:        "/bin/sh -c accelerate launch /workspace/tfs/inference_api.py",
			hasAdapters:        true,
			expectedVolume:     "adapter-volume",
			expectedEnvVars: []corev1.EnvVar{{
				Name:  "Adapter-1",
				Value: "0.5",
			}},
		},

		"test-model-download/vllm": {
			workspace: test.MockWorkspaceWithPresetDownloadVLLM,
			nodeCount: 1,
			modelName: "test-model-download",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
			},
			workload:    "Deployment",
			expectedCmd: `/bin/sh -c python3 /workspace/vllm/inference_api.py --tensor-parallel-size=2 --model=test-repo/test-model --code-revision=test-revision --gpu-memory-utilization=0.90 --download-dir=/workspace/weights --kaito-config-file=/mnt/config/inference_config.yaml`,
			expectedEnvVars: []corev1.EnvVar{{
				Name: "HF_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key: "HF_TOKEN",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
					},
				},
			}},
		},

		"test-model-download-distributed/vllm": {
			workspace: test.MockWorkspaceWithPresetDownloadVLLM,
			nodeCount: 2,
			modelName: "test-model-download",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			workload:    "StatefulSet",
			expectedCmd: `/bin/sh -c if [ "${POD_INDEX}" = "0" ]; then  --ray_cluster_size=2 --ray_port=6379; python3 /workspace/vllm/inference_api.py --model=test-repo/test-model --code-revision=test-revision --gpu-memory-utilization=0.90 --download-dir=/workspace/weights --kaito-config-file=/mnt/config/inference_config.yaml --pipeline-parallel-size=2 --tensor-parallel-size=2; else  --ray_address=testWorkspace-0.testWorkspace-headless.kaito.svc.cluster.local --ray_port=6379; fi`,
			expectedEnvVars: []corev1.EnvVar{{
				Name: "HF_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key: "HF_TOKEN",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
					},
				},
			}, {
				Name: "POD_INDEX",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: fmt.Sprintf("metadata.labels['%s']", appsv1.PodIndexLabel),
					},
				},
			}},
		},

		"test-model-download-distributed/vllm (more nodes than required)": {
			// Using Standard_NC12s_v3, which has 32GB GPU memory per node.
			// The preset requires 64GB GPU memory for the model, so only 2 nodes are needed.
			workspace: test.MockWorkspaceWithPresetDownloadVLLM,
			nodeCount: 4, // 4 nodes but only 2 are needed
			modelName: "test-model-download",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			workload:    "StatefulSet",
			expectedCmd: `/bin/sh -c if [ "${POD_INDEX}" = "0" ]; then  --ray_cluster_size=2 --ray_port=6379; python3 /workspace/vllm/inference_api.py --model=test-repo/test-model --code-revision=test-revision --gpu-memory-utilization=0.90 --kaito-config-file=/mnt/config/inference_config.yaml --download-dir=/workspace/weights --pipeline-parallel-size=2 --tensor-parallel-size=2; else  --ray_address=testWorkspace-0.testWorkspace-headless.kaito.svc.cluster.local --ray_port=6379; fi`,
			expectedEnvVars: []corev1.EnvVar{{
				Name: "HF_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key: "HF_TOKEN",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
					},
				},
			}, {
				Name: "POD_INDEX",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: fmt.Sprintf("metadata.labels['%s']", appsv1.PodIndexLabel),
					},
				},
			}},
		},

		"test-model-download/transformers": {
			workspace: test.MockWorkspaceWithPresetDownloadTransformers,
			nodeCount: 1,
			modelName: "test-model-download",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
			},
			workload:    "Deployment",
			expectedCmd: "/bin/sh -c accelerate launch /workspace/tfs/inference_api.py --pretrained_model_name_or_path=test-repo/test-model --revision=test-revision",
			expectedEnvVars: []corev1.EnvVar{{
				Name: "HF_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key: "HF_TOKEN",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-secret",
						},
					},
				},
			}},
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
			t.Setenv("PRESET_REGISTRY_NAME", "test-registry")

			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			workspace := tc.workspace
			workspace.Resource.Count = &tc.nodeCount
			expectedSecrets := []string{"fake-secret"}
			if tc.hasAdapters {
				workspace.Inference.Adapters = []v1beta1.AdapterSpec{
					{
						Source: &v1beta1.DataSource{
							Name:             "Adapter-1",
							Image:            "fake.kaito.com/kaito-image:0.0.1",
							ImagePullSecrets: expectedSecrets,
						},
						Strength: &ValidStrength,
					},
				}
			} else {
				workspace.Inference.Adapters = nil
			}

			model := plugin.KaitoModelRegister.MustGet(tc.modelName)

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

			createdObject, _ := GeneratePresetInference(context.TODO(), workspace, test.MockWorkspaceWithPresetHash, model, mockClient)
			createdWorkload := ""
			image := ""
			envVars := []corev1.EnvVar{}
			var initContainer []corev1.Container
			switch t := createdObject.(type) {
			case *appsv1.Deployment:
				createdWorkload = "Deployment"
				image = t.Spec.Template.Spec.Containers[0].Image
				envVars = t.Spec.Template.Spec.Containers[0].Env
				initContainer = t.Spec.Template.Spec.InitContainers
			case *appsv1.StatefulSet:
				createdWorkload = "StatefulSet"
				image = t.Spec.Template.Spec.Containers[0].Image
				envVars = t.Spec.Template.Spec.Containers[0].Env
				initContainer = t.Spec.Template.Spec.InitContainers
			}
			if tc.workload != createdWorkload {
				t.Errorf("%s: returned workload type is wrong", k)
			}

			if tc.expectedModelImage != "" {
				var pullerContainer corev1.Container
				for _, container := range initContainer {
					if container.Name == "model-weights-downloader" {
						pullerContainer = container
						break
					}
				}
				expectedPullerCmd := []string{
					"oras",
					"pull",
					tc.expectedModelImage,
					"-o",
					utils.DefaultWeightsVolumePath,
				}
				if !reflect.DeepEqual(pullerContainer.Command, expectedPullerCmd) {
					t.Errorf("%s: Puller command is not expected, got %v, expect %v", k, pullerContainer.Command, expectedPullerCmd)
				}
			}
			if image != baseImageName {
				t.Errorf("%s: image is not expected, got %s, expect %s", k, image, baseImageName)
			}

			if !reflect.DeepEqual(envVars, tc.expectedEnvVars) {
				t.Errorf("%s: EnvVars are not expected, got %v, expect %v", k, envVars, tc.expectedEnvVars)
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
			expectedParams := toParameterMap(strings.Split(tc.expectedCmd, "--")[1:])

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

func TestGetDistributedInferenceProbe(t *testing.T) {
	testcases := map[string]struct {
		probeType           probeType
		workspace           *v1beta1.Workspace
		initialDelaySeconds int32
		periodSeconds       int32
		timeoutSeconds      int32
		expectedProbe       *corev1.Probe
	}{
		"Liveness": {
			probeType: probeTypeLiveness,
			workspace: &v1beta1.Workspace{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "test-ns",
				},
			},
			initialDelaySeconds: 30,
			periodSeconds:       5,
			timeoutSeconds:      5,
			expectedProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{"/bin/sh", "-c", "python3 /workspace/vllm/multi-node-health-check.py liveness --leader-address=test-workspace-0.test-workspace-headless.test-ns.svc.cluster.local --ray-port=6379"},
					},
				},
				InitialDelaySeconds:           30,
				PeriodSeconds:                 5,
				TimeoutSeconds:                5,
				TerminationGracePeriodSeconds: lo.ToPtr(int64(1)),
				FailureThreshold:              1,
			},
		},
		"Readiness": {
			probeType: probeTypeReadiness,
			workspace: &v1beta1.Workspace{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "test-ns",
				},
			},
			initialDelaySeconds: 30,
			periodSeconds:       5,
			timeoutSeconds:      5,
			expectedProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{"/bin/sh", "-c", "python3 /workspace/vllm/multi-node-health-check.py readiness --leader-address=test-workspace-0.test-workspace-headless.test-ns.svc.cluster.local --vllm-port=5000"},
					},
				},
				InitialDelaySeconds: 30,
				PeriodSeconds:       5,
				TimeoutSeconds:      5,
				FailureThreshold:    1,
			},
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			actualProbe := getDistributedInferenceProbe(tc.probeType, tc.workspace, tc.initialDelaySeconds, tc.periodSeconds, tc.timeoutSeconds)
			if actualProbe.Exec != nil && tc.expectedProbe.Exec != nil {
				expected := toParameterMap(tc.expectedProbe.Exec.Command)
				actual := toParameterMap(actualProbe.Exec.Command)
				if !reflect.DeepEqual(expected, actual) {
					t.Errorf("Exec command mismatch: expected %+v, got %+v", expected, actual)
				}
			}
			if actualProbe.HTTPGet != nil && tc.expectedProbe.HTTPGet != nil {
				if !reflect.DeepEqual(actualProbe.HTTPGet, tc.expectedProbe.HTTPGet) {
					t.Errorf("HTTPGet mismatch: expected %+v, got %+v", tc.expectedProbe.HTTPGet, actualProbe.HTTPGet)
				}
			}
		})
	}
}

func toParameterMap(in []string) map[string]string {
	ret := make(map[string]string)
	for _, eachToken := range in {
		for _, each := range strings.Split(eachToken, " ") {
			each = strings.TrimSpace(each)
			each = strings.Trim(each, ";")
			if len(each) == 0 {
				continue
			}
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
	}
	return ret
}
