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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
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
				ObjectMeta: metav1.ObjectMeta{
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
				ObjectMeta: metav1.ObjectMeta{
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
				ObjectMeta: metav1.ObjectMeta{
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

func TestGetGPUConfig(t *testing.T) {
	test.RegisterTestModel()

	testcases := map[string]struct {
		workspace      *v1beta1.Workspace
		model          string
		callMocks      func(c *test.MockClient)
		expectedConfig sku.GPUConfig
	}{
		"SKU path - get config from instanceType": {
			workspace: &v1beta1.Workspace{
				Resource: v1beta1.ResourceSpec{
					InstanceType:   "Standard_NC24ads_A100_v4",
					PreferredNodes: []string{},
					LabelSelector:  &metav1.LabelSelector{},
				},
			},
			model: "test-model",
			callMocks: func(c *test.MockClient) {
				// No need for mock expectations here
			},
			expectedConfig: sku.GPUConfig{
				SKU:             "Standard_NC24ads_A100_v4",
				GPUCount:        1,
				GPUMemGB:        80,
				GPUModel:        "NVIDIA A100",
				NVMeDiskEnabled: true,
			},
		},
		"Node status path - get config from node": {
			workspace: &v1beta1.Workspace{
				Resource: v1beta1.ResourceSpec{
					InstanceType:   "unknown-instance-type",
					PreferredNodes: []string{},
					LabelSelector:  &metav1.LabelSelector{},
				},
				Status: v1beta1.WorkspaceStatus{
					WorkerNodes: []string{"gpu-node-1"},
				},
			},
			model: "test-model",
			callMocks: func(c *test.MockClient) {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-node-1",
					},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							consts.NvidiaGPU: *resource.NewQuantity(2, resource.DecimalSI),
						},
					},
				}
				c.On("Get", mock.Anything, mock.MatchedBy(func(key client.ObjectKey) bool {
					return key.Name == "gpu-node-1"
				}), mock.IsType(&corev1.Node{}), mock.Anything).
					Run(func(args mock.Arguments) {
						nodeArg := args.Get(2).(*corev1.Node)
						*nodeArg = *node
					}).Return(nil)
			},
			expectedConfig: sku.GPUConfig{
				SKU:      "unknown",
				GPUCount: 2,
				GPUModel: "unknown",
			},
		},
		"Fallback path when node fetch fails": {
			workspace: &v1beta1.Workspace{
				Resource: v1beta1.ResourceSpec{
					InstanceType:   "unknown-instance-type",
					PreferredNodes: []string{},
					LabelSelector:  &metav1.LabelSelector{},
				},
				Status: v1beta1.WorkspaceStatus{
					WorkerNodes: []string{"gpu-node-1"},
				},
			},
			model: "test-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.Anything, mock.MatchedBy(func(key client.ObjectKey) bool {
					return key.Name == "gpu-node-1"
				}), mock.IsType(&corev1.Node{}), mock.Anything).Return(fmt.Errorf("no nodes found"))
			},
			expectedConfig: sku.GPUConfig{
				GPUCount: 1, // From test-model's GPUCountRequirement
			},
		},
		"PreferredNodes path - bypass SKU path": {
			workspace: &v1beta1.Workspace{
				Resource: v1beta1.ResourceSpec{
					InstanceType:   "Standard_NC24ads_A100_v4",
					PreferredNodes: []string{"preferred-node"},
					LabelSelector:  &metav1.LabelSelector{},
				},
				Status: v1beta1.WorkspaceStatus{
					WorkerNodes: []string{"gpu-node-1"},
				},
			},
			model: "test-model",
			callMocks: func(c *test.MockClient) {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-node-1",
					},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							consts.NvidiaGPU: *resource.NewQuantity(2, resource.DecimalSI),
						},
					},
				}
				c.On("Get", mock.Anything, mock.MatchedBy(func(key client.ObjectKey) bool {
					return key.Name == "gpu-node-1"
				}), mock.IsType(&corev1.Node{}), mock.Anything).
					Run(func(args mock.Arguments) {
						nodeArg := args.Get(2).(*corev1.Node)
						*nodeArg = *node
					}).Return(nil)
			},
			expectedConfig: sku.GPUConfig{
				SKU:      "unknown",
				GPUCount: 2,
				GPUModel: "unknown",
			},
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			model := plugin.KaitoModelRegister.MustGet(tc.model)

			gctx := &generator.WorkspaceGeneratorContext{
				Ctx:        context.TODO(),
				Workspace:  tc.workspace,
				Model:      model,
				KubeClient: mockClient,
			}

			config := getGPUConfig(gctx)

			// Check each field against expected values
			if tc.expectedConfig.SKU != "" && config.SKU != tc.expectedConfig.SKU {
				t.Errorf("Expected SKU %s, got %s", tc.expectedConfig.SKU, config.SKU)
			}

			// GPUCount should always be verified
			if config.GPUCount != tc.expectedConfig.GPUCount {
				t.Errorf("Expected GPUCount %d, got %d", tc.expectedConfig.GPUCount, config.GPUCount)
			}

			// Check GPUModel if expected
			if tc.expectedConfig.GPUModel != "" && config.GPUModel != tc.expectedConfig.GPUModel {
				t.Errorf("Expected GPUModel %s, got %s", tc.expectedConfig.GPUModel, config.GPUModel)
			}

			// Check GPUMemGB if expected
			if tc.expectedConfig.GPUMemGB > 0 && config.GPUMemGB != tc.expectedConfig.GPUMemGB {
				t.Errorf("Expected GPUMemGB %d, got %d", tc.expectedConfig.GPUMemGB, config.GPUMemGB)
			}

			// Check NVMeDiskEnabled if expected
			if tc.expectedConfig.NVMeDiskEnabled && !config.NVMeDiskEnabled {
				t.Errorf("Expected NVMeDiskEnabled to be true, got false")
			}
		})
	}
}
func TestSetAdapterPuller(t *testing.T) {
	testcases := map[string]struct {
		workspace       *v1beta1.Workspace
		spec            *corev1.PodSpec
		expectedVolumes []string
		expectedEnvVars []string
		expectError     bool
	}{
		"no adapters": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Inference: &v1beta1.InferenceSpec{
					Adapters: []v1beta1.AdapterSpec{}, // empty adapters
				},
			},
			spec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "test-container",
					},
				},
			},
			expectedVolumes: []string{},
			expectedEnvVars: []string{},
			expectError:     false,
		},
		"single adapter": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Inference: &v1beta1.InferenceSpec{
					Adapters: []v1beta1.AdapterSpec{
						{
							Source: &v1beta1.DataSource{
								Name:  "adapter-1",
								Image: "test-registry/adapter:v1",
							},
							Strength: &ValidStrength,
						},
					},
				},
			},
			spec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "test-container",
					},
				},
			},
			expectedVolumes: []string{"adapter-volume"},
			expectedEnvVars: []string{"adapter-1"},
			expectError:     false,
		},
		"multiple adapters": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Inference: &v1beta1.InferenceSpec{
					Adapters: []v1beta1.AdapterSpec{
						{
							Source: &v1beta1.DataSource{
								Name:  "adapter-1",
								Image: "test-registry/adapter1:v1",
							},
							Strength: &ValidStrength,
						},
						{
							Source: &v1beta1.DataSource{
								Name:  "adapter-2",
								Image: "test-registry/adapter2:v1",
							},
							Strength: &ValidStrength,
						},
					},
				},
			},
			spec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "test-container",
					},
				},
			},
			expectedVolumes: []string{"adapter-volume"},
			expectedEnvVars: []string{"adapter-1", "adapter-2"},
			expectError:     false,
		},
		"adapters with image pull secrets": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Inference: &v1beta1.InferenceSpec{
					Adapters: []v1beta1.AdapterSpec{
						{
							Source: &v1beta1.DataSource{
								Name:             "adapter-1",
								Image:            "private-registry/adapter:v1",
								ImagePullSecrets: []string{"my-secret"},
							},
							Strength: &ValidStrength,
						},
					},
				},
			},
			spec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "test-container",
					},
				},
			},
			expectedVolumes: []string{"adapter-volume", "docker-config-adapter-1-inference-adapter"},
			expectedEnvVars: []string{"adapter-1"},
			expectError:     false,
		},
		"multiple containers": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Inference: &v1beta1.InferenceSpec{
					Adapters: []v1beta1.AdapterSpec{
						{
							Source: &v1beta1.DataSource{
								Name:  "adapter-1",
								Image: "test-registry/adapter:v1",
							},
							Strength: &ValidStrength,
						},
					},
				},
			},
			spec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "container-1",
					},
					{
						Name: "container-2",
					},
				},
			},
			expectedVolumes: []string{"adapter-volume"},
			expectedEnvVars: []string{"adapter-1"},
			expectError:     false,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			ctx := &generator.WorkspaceGeneratorContext{
				Ctx:       context.TODO(),
				Workspace: tc.workspace,
			}

			err := SetAdapterPuller(ctx, tc.spec)

			if tc.expectError && err == nil {
				t.Errorf("expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Check volumes
			actualVolumes := make([]string, 0)
			for _, volume := range tc.spec.Volumes {
				actualVolumes = append(actualVolumes, volume.Name)
			}
			if !reflect.DeepEqual(actualVolumes, tc.expectedVolumes) {
				t.Errorf("volumes mismatch: expected %v, got %v", tc.expectedVolumes, actualVolumes)
			}

			// Check volume mounts on containers
			if len(tc.expectedVolumes) > 0 {
				for _, container := range tc.spec.Containers {
					foundAdapterMount := false
					for _, mount := range container.VolumeMounts {
						if mount.Name == "adapter-volume" {
							foundAdapterMount = true
							if mount.MountPath != "/mnt/adapter" {
								t.Errorf("unexpected mount path for adapter volume: %s", mount.MountPath)
							}
							break
						}
					}
					if !foundAdapterMount {
						t.Errorf("adapter volume mount not found in container %s", container.Name)
					}
				}
			}

			// Check environment variables
			if len(tc.expectedEnvVars) > 0 {
				for _, container := range tc.spec.Containers {
					actualEnvVarNames := make([]string, 0)
					for _, env := range container.Env {
						actualEnvVarNames = append(actualEnvVarNames, env.Name)
					}

					// Check if all expected env vars are present
					for _, expectedVar := range tc.expectedEnvVars {
						found := false
						for _, actualVar := range actualEnvVarNames {
							if actualVar == expectedVar {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("expected env var %s not found in container %s", expectedVar, container.Name)
						}
					}
				}
			}

			// Check init containers
			if len(tc.workspace.Inference.Adapters) > 0 {
				if len(tc.spec.InitContainers) == 0 {
					t.Errorf("expected init containers for adapter pulling but found none")
				}
			}
		})
	}
}

func TestSetModelDownloadInfo(t *testing.T) {
	test.RegisterTestModel()

	testcases := map[string]struct {
		workspace             *v1beta1.Workspace
		modelName             string
		spec                  *corev1.PodSpec
		expectedEnvVars       []corev1.EnvVar
		expectedInitContainer int
		expectError           bool
		expectedErrorMsg      string
	}{
		"download at runtime - add HF_TOKEN": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Inference: &v1beta1.InferenceSpec{
					Preset: &v1beta1.PresetSpec{
						PresetMeta: v1beta1.PresetMeta{
							Name: "test-model-download",
						},
						PresetOptions: v1beta1.PresetOptions{
							ModelAccessSecret: "hf-secret",
						},
					},
				},
			},
			modelName: "test-model-download",
			spec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "container-1",
					},
				},
			},
			expectedEnvVars: []corev1.EnvVar{
				{
					Name: "HF_TOKEN",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "hf-secret",
							},
							Key: "HF_TOKEN",
						},
					},
				},
			},
			expectedInitContainer: 0,
			expectError:           false,
		},
		"model puller - add init containers": {
			workspace: &v1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Inference: &v1beta1.InferenceSpec{
					Preset: &v1beta1.PresetSpec{
						PresetMeta: v1beta1.PresetMeta{
							Name: "test-model",
						},
					},
				},
			},
			modelName: "test-model",
			spec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "test-container",
					},
				},
			},
			expectedEnvVars:       []corev1.EnvVar{},
			expectedInitContainer: 1, // Expecting model-weights-downloader
			expectError:           false,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			model := plugin.KaitoModelRegister.MustGet(tc.modelName)

			ctx := &generator.WorkspaceGeneratorContext{
				Ctx:       context.TODO(),
				Workspace: tc.workspace,
				Model:     model,
			}

			// Store original state
			originalInitContainerCount := len(tc.spec.InitContainers)

			err := SetModelDownloadInfo(ctx, tc.spec)

			// Check error
			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
				} else if tc.expectedErrorMsg != "" && err.Error() != tc.expectedErrorMsg {
					t.Errorf("expected error message %q, got %q", tc.expectedErrorMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Skip further checks if error is expected
			if tc.expectError {
				return
			}

			// Check environment variables if expected
			if len(tc.expectedEnvVars) > 0 {
				for i, container := range tc.spec.Containers {
					found := false
					for _, env := range container.Env {
						if env.Name == "HF_TOKEN" {
							found = true
							if !reflect.DeepEqual(env, tc.expectedEnvVars[0]) {
								t.Errorf("container %d: HF_TOKEN env var mismatch: expected %+v, got %+v",
									i, tc.expectedEnvVars[0], env)
							}
						}
					}
					if !found {
						t.Errorf("container %d: HF_TOKEN env var not found", i)
					}
				}
			} else {
				// Verify no HF_TOKEN env vars were added
				for i, container := range tc.spec.Containers {
					for _, env := range container.Env {
						if env.Name == "HF_TOKEN" {
							t.Errorf("container %d: unexpected HF_TOKEN env var found", i)
						}
					}
				}
			}

			// Check init containers
			actualInitContainerCount := len(tc.spec.InitContainers) - originalInitContainerCount
			if actualInitContainerCount != tc.expectedInitContainer {
				t.Errorf("init container count mismatch: expected %d new containers, got %d",
					tc.expectedInitContainer, actualInitContainerCount)
			}

			if tc.expectedInitContainer > 0 {
				// Look for model-weights-downloader container
				found := false
				for _, initContainer := range tc.spec.InitContainers {
					if initContainer.Name == "model-weights-downloader" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected model-weights-downloader init container not found")
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
