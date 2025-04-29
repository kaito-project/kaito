// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package inference

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

var ValidStrength string = "0.5"

func TestCreatePresetInference(t *testing.T) {
	test.RegisterTestModel()
	testcases := map[string]struct {
		workspace       *v1beta1.Workspace
		nodeCount       int
		modelName       string
		callMocks       func(c *test.MockClient)
		workload        string
		expectedCmd     string
		hasAdapters     bool
		expectedImage   string
		expectedVolume  string
		expectedEnvVars []corev1.EnvVar
	}{

		"test-model/vllm": {
			workspace: test.MockWorkspaceWithPresetVLLM,
			nodeCount: 1,
			modelName: "test-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			workload:      "Deployment",
			expectedImage: "test-registry/kaito-test-model:base-test-model",
			// No BaseCommand, TorchRunParams, TorchRunRdzvParams, or ModelRunParams
			// So expected cmd consists of shell command and inference file
			expectedCmd: "/bin/sh -c python3 /workspace/vllm/inference_api.py --tensor-parallel-size=2 --served-model-name=mymodel --gpu-memory-utilization=0.90 --kaito-config-file=/mnt/config/inference_config.yaml",
			hasAdapters: false,
			expectedEnvVars: []corev1.EnvVar{{
				Name:  "PYTORCH_CUDA_ALLOC_CONF",
				Value: "expandable_segments:True",
			}},
		},

		"test-model-no-parallel/vllm": {
			workspace: test.MockWorkspaceWithPresetVLLM,
			nodeCount: 1,
			modelName: "test-no-tensor-parallel-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			workload:      "Deployment",
			expectedImage: "test-registry/kaito-test-model:test-no-tensor-parallel-model",
			// No BaseCommand, TorchRunParams, TorchRunRdzvParams, or ModelRunParams
			// So expected cmd consists of shell command and inference file
			expectedCmd: "/bin/sh -c python3 /workspace/vllm/inference_api.py --kaito-config-file=/mnt/config/inference_config.yaml --gpu-memory-utilization=0.90",
			hasAdapters: false,
			expectedEnvVars: []corev1.EnvVar{{
				Name:  "PYTORCH_CUDA_ALLOC_CONF",
				Value: "expandable_segments:True",
			}},
		},

		"test-model-no-lora-support/vllm": {
			workspace: test.MockWorkspaceWithPresetVLLM,
			nodeCount: 1,
			modelName: "test-no-lora-support-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			workload:      "Deployment",
			expectedImage: "test-registry/kaito-test-model:test-no-lora-support-model",
			// No BaseCommand, TorchRunParams, TorchRunRdzvParams, or ModelRunParams
			// So expected cmd consists of shell command and inference file
			expectedCmd: "/bin/sh -c python3 /workspace/vllm/inference_api.py --kaito-config-file=/mnt/config/inference_config.yaml --gpu-memory-utilization=0.90",
			hasAdapters: false,
			expectedEnvVars: []corev1.EnvVar{{
				Name:  "PYTORCH_CUDA_ALLOC_CONF",
				Value: "expandable_segments:True",
			}},
		},

		"test-model-with-adapters/vllm": {
			workspace: test.MockWorkspaceWithPresetVLLM,
			nodeCount: 1,
			modelName: "test-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			workload:       "Deployment",
			expectedImage:  "test-registry/kaito-test-model:base-test-model",
			expectedCmd:    "/bin/sh -c python3 /workspace/vllm/inference_api.py --enable-lora --tensor-parallel-size=2 --served-model-name=mymodel --gpu-memory-utilization=0.90 --kaito-config-file=/mnt/config/inference_config.yaml",
			hasAdapters:    true,
			expectedVolume: "adapter-volume",
			expectedEnvVars: []corev1.EnvVar{{
				Name:  "PYTORCH_CUDA_ALLOC_CONF",
				Value: "expandable_segments:True",
			}, {
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
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			workload:      "Deployment",
			expectedImage: "test-registry/kaito-test-model:base-test-model",
			// No BaseCommand, TorchRunParams, TorchRunRdzvParams, or ModelRunParams
			// So expected cmd consists of shell command and inference file
			expectedCmd: "/bin/sh -c accelerate launch /workspace/tfs/inference_api.py",
			hasAdapters: false,
			expectedEnvVars: []corev1.EnvVar{{
				Name:  "PYTORCH_CUDA_ALLOC_CONF",
				Value: "expandable_segments:True",
			}},
		},

		"test-distributed-model/transformers": {
			workspace: test.MockWorkspaceDistributedModel,
			nodeCount: 1,
			modelName: "test-distributed-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.StatefulSet{}), mock.Anything).Return(nil)
			},
			workload:      "StatefulSet",
			expectedImage: "test-registry/kaito-test-distributed-model:base-test-model",
			expectedCmd:   "/bin/sh -c accelerate launch --nnodes=1 --nproc_per_node=0 --max_restarts=3 --rdzv_id=job --rdzv_backend=c10d --rdzv_endpoint=testWorkspace-0.testWorkspace-headless.kaito.svc.cluster.local:29500 /workspace/tfs/inference_api.py",
			hasAdapters:   false,
			expectedEnvVars: []corev1.EnvVar{{
				Name:  "PYTORCH_CUDA_ALLOC_CONF",
				Value: "expandable_segments:True",
			}},
		},

		"test-model-with-adapters": {
			workspace: test.MockWorkspaceWithPreset,
			nodeCount: 1,
			modelName: "test-model",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			workload:       "Deployment",
			expectedImage:  "test-registry/kaito-test-model:base-test-model",
			expectedCmd:    "/bin/sh -c accelerate launch /workspace/tfs/inference_api.py",
			hasAdapters:    true,
			expectedVolume: "adapter-volume",
			expectedEnvVars: []corev1.EnvVar{{
				Name:  "PYTORCH_CUDA_ALLOC_CONF",
				Value: "expandable_segments:True",
			}, {
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
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			workload:      "Deployment",
			expectedImage: "test-registry/kaito-base:0.0.1",
			expectedCmd:   "/bin/sh -c python3 /workspace/vllm/inference_api.py --gpu-memory-utilization=0.90 --kaito-config-file=/mnt/config/inference_config.yaml --model=test-repo/test-model --code-revision=test-revision --tensor-parallel-size=2",
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
				Name:  "PYTORCH_CUDA_ALLOC_CONF",
				Value: "expandable_segments:True",
			}},
		},

		"test-model-download/transformers": {
			workspace: test.MockWorkspaceWithPresetDownloadTransformers,
			nodeCount: 1,
			modelName: "test-model-download",
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.TODO()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.TODO()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			workload:      "Deployment",
			expectedImage: "test-registry/kaito-base:0.0.1",
			expectedCmd:   "/bin/sh -c accelerate launch /workspace/tfs/inference_api.py --pretrained_model_name_or_path=test-repo/test-model --revision=test-revision",
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
				Name:  "PYTORCH_CUDA_ALLOC_CONF",
				Value: "expandable_segments:True",
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

			createdObject, _ := CreatePresetInference(context.TODO(), workspace, test.MockWorkspaceWithPresetHash, model, mockClient)
			createdWorkload := ""
			image := ""
			envVars := []corev1.EnvVar{}
			switch t := createdObject.(type) {
			case *appsv1.Deployment:
				createdWorkload = "Deployment"
				image = t.Spec.Template.Spec.Containers[0].Image
				envVars = t.Spec.Template.Spec.Containers[0].Env
			case *appsv1.StatefulSet:
				createdWorkload = "StatefulSet"
				image = t.Spec.Template.Spec.Containers[0].Image
				envVars = t.Spec.Template.Spec.Containers[0].Env
			}
			if tc.workload != createdWorkload {
				t.Errorf("%s: returned workload type is wrong", k)
			}
			if image != tc.expectedImage {
				t.Errorf("%s: Image is not expected, got %s, expect %s", k, image, tc.expectedImage)
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

func TestParseModelVersion(t *testing.T) {
	testCases := []struct {
		name             string
		version          string
		expectedRepoId   string
		expectedRevision string
	}{
		{
			name:             "Valid URL with revision",
			version:          "https://huggingface.co/tiiuae/falcon-7b/commit/ec89142b67d748a1865ea4451372db8313ada0d8",
			expectedRepoId:   "tiiuae/falcon-7b",
			expectedRevision: "ec89142b67d748a1865ea4451372db8313ada0d8",
		},
		{
			name:             "Valid URL without revision",
			version:          "https://huggingface.co/tiiuae/falcon-7b",
			expectedRepoId:   "tiiuae/falcon-7b",
			expectedRevision: "",
		},
		{
			name:             "Valid URL with trailing slash",
			version:          "https://huggingface.co/tiiuae/falcon-7b/",
			expectedRepoId:   "tiiuae/falcon-7b",
			expectedRevision: "",
		},
		{
			name:             "Valid URL with revision and trailing slash",
			version:          "https://huggingface.co/tiiuae/falcon-7b/commit/ec89142b67d748a1865ea4451372db8313ada0d8/",
			expectedRepoId:   "tiiuae/falcon-7b",
			expectedRevision: "ec89142b67d748a1865ea4451372db8313ada0d8",
		},
		{
			name:             "Invalid URL format",
			version:          "invalid-url",
			expectedRepoId:   "",
			expectedRevision: "",
		},
		{
			name:             "Empty string input",
			version:          "",
			expectedRepoId:   "",
			expectedRevision: "",
		},
		{
			name:             "URL with extra path segments",
			version:          "https://huggingface.co/org/model/extra/path/commit/revision",
			expectedRepoId:   "",
			expectedRevision: "",
		},
		{
			name:             "URL without commit part but revision-like segment",
			version:          "https://huggingface.co/org/model/revision",
			expectedRepoId:   "", // Expect empty because path structure is not /org/model/commit/revision or /org/model
			expectedRevision: "",
		},
		{
			name:             "URL with only org",
			version:          "https://huggingface.co/org/",
			expectedRepoId:   "",
			expectedRevision: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			repoId, revision := parseModelVersion(tc.version)
			if repoId != tc.expectedRepoId {
				t.Errorf("Expected repoId %q, but got %q", tc.expectedRepoId, repoId)
			}
			if revision != tc.expectedRevision {
				t.Errorf("Expected revision %q, but got %q", tc.expectedRevision, revision)
			}
		})
	}
}

func toParameterMap(in []string) map[string]string {
	ret := make(map[string]string)
	for _, eachToken := range in {
		for _, each := range strings.Split(eachToken, " ") {
			each = strings.TrimSpace(each)
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
