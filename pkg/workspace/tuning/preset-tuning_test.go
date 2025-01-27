// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package tuning

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"
)

func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Saves state of current env, and returns function to restore to saved state
func saveEnv(key string) func() {
	envVal, envExists := os.LookupEnv(key)
	return func() {
		if envExists {
			err := os.Setenv(key, envVal)
			if err != nil {
				return
			}
		} else {
			err := os.Unsetenv(key)
			if err != nil {
				return
			}
		}
	}
}

func TestGetInstanceGPUCount(t *testing.T) {
	os.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	testcases := map[string]struct {
		sku              string
		expectedGPUCount int
	}{
		"SKU Exists With Multiple GPUs": {
			sku:              "Standard_NC24s_v3",
			expectedGPUCount: 4,
		},
		"SKU Exists With One GPU": {
			sku:              "Standard_NC6s_v3",
			expectedGPUCount: 1,
		},
		"SKU Does Not Exist": {
			sku:              "sku_unknown",
			expectedGPUCount: 1,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			result := getInstanceGPUCount(tc.sku)
			assert.Equal(t, tc.expectedGPUCount, result)
		})
	}
}

func TestGetTuningImageInfo(t *testing.T) {
	// Setting up test environment
	originalRegistryName := os.Getenv("PRESET_REGISTRY_NAME")
	defer func() {
		os.Setenv("PRESET_REGISTRY_NAME", originalRegistryName) // Reset after tests
	}()

	testcases := map[string]struct {
		registryName string
		wObj         *kaitov1alpha1.Workspace
		presetObj    *model.PresetParam
		expected     string
	}{
		"Valid Registry and Parameters": {
			registryName: "testregistry",
			wObj: &kaitov1alpha1.Workspace{
				Tuning: &kaitov1alpha1.TuningSpec{
					Preset: &kaitov1alpha1.PresetSpec{
						PresetMeta: kaitov1alpha1.PresetMeta{
							Name: "testpreset",
						},
					},
				},
			},
			presetObj: &model.PresetParam{
				Tag: "latest",
			},
			expected: "testregistry/kaito-testpreset:latest",
		},
		"Empty Registry Name": {
			registryName: "",
			wObj: &kaitov1alpha1.Workspace{
				Tuning: &kaitov1alpha1.TuningSpec{
					Preset: &kaitov1alpha1.PresetSpec{
						PresetMeta: kaitov1alpha1.PresetMeta{
							Name: "testpreset",
						},
					},
				},
			},
			presetObj: &model.PresetParam{
				Tag: "latest",
			},
			expected: "/kaito-testpreset:latest",
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			os.Setenv("PRESET_REGISTRY_NAME", tc.registryName)
			result, _ := GetTuningImageInfo(context.Background(), tc.wObj, tc.presetObj)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetDataSrcImageInfo(t *testing.T) {
	testcases := map[string]struct {
		wObj            *kaitov1alpha1.Workspace
		expectedImage   string
		expectedSecrets []corev1.LocalObjectReference
	}{
		"Multiple Image Pull Secrets": {
			wObj: &kaitov1alpha1.Workspace{
				Tuning: &kaitov1alpha1.TuningSpec{
					Input: &kaitov1alpha1.DataSource{
						Image:            "kaito/data-source",
						ImagePullSecrets: []string{"secret1", "secret2"},
					},
				},
			},
			expectedImage: "kaito/data-source",
			expectedSecrets: []corev1.LocalObjectReference{
				{Name: "secret1"},
				{Name: "secret2"},
			},
		},
		"No Image Pull Secrets": {
			wObj: &kaitov1alpha1.Workspace{
				Tuning: &kaitov1alpha1.TuningSpec{
					Input: &kaitov1alpha1.DataSource{
						Image: "kaito/data-source",
					},
				},
			},
			expectedImage:   "kaito/data-source",
			expectedSecrets: []corev1.LocalObjectReference{},
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			resultImage, resultSecrets := GetDataSrcImageInfo(context.Background(), tc.wObj)
			assert.Equal(t, tc.expectedImage, resultImage)
			assert.Equal(t, tc.expectedSecrets, resultSecrets)
		})
	}
}

func TestEnsureTuningConfigMap(t *testing.T) {
	testcases := map[string]struct {
		setupEnv      func()
		callMocks     func(c *test.MockClient)
		workspaceObj  *kaitov1alpha1.Workspace
		expectedError string
	}{
		"Config already exists in workspace namespace": {
			setupEnv: func() {
				os.Setenv(consts.DefaultReleaseNamespaceEnvVar, "release-namespace")
			},
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
			},
			workspaceObj: &kaitov1alpha1.Workspace{
				Tuning: &kaitov1alpha1.TuningSpec{
					Config: "config-template",
				},
			},
			expectedError: "",
		},
		"Error finding release namespace": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(errors.NewNotFound(schema.GroupResource{}, "config-template"))
			},
			workspaceObj: &kaitov1alpha1.Workspace{
				Tuning: &kaitov1alpha1.TuningSpec{
					Config: "config-template",
				},
			},
			expectedError: "failed to get release namespace: failed to determine release namespace from file /var/run/secrets/kubernetes.io/serviceaccount/namespace and env var RELEASE_NAMESPACE",
		},
		"Config doesn't exist in template namespace": {
			setupEnv: func() {
				os.Setenv(consts.DefaultReleaseNamespaceEnvVar, "release-namespace")
			},
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(errors.NewNotFound(schema.GroupResource{}, "config-template"))
			},
			workspaceObj: &kaitov1alpha1.Workspace{
				Tuning: &kaitov1alpha1.TuningSpec{
					Config: "config-template",
				},
			},
			expectedError: "failed to get ConfigMap from template namespace:  \"config-template\" not found",
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			cleanupEnv := saveEnv(consts.DefaultReleaseNamespaceEnvVar)
			defer cleanupEnv()

			if tc.setupEnv != nil {
				tc.setupEnv()
			}
			mockClient := test.NewClient()
			tc.callMocks(mockClient)
			tc.workspaceObj.SetNamespace("workspace-namespace")
			_, err := EnsureTuningConfigMap(context.Background(), tc.workspaceObj, mockClient)
			if tc.expectedError != "" {
				assert.EqualError(t, err, tc.expectedError)
			} else {
				assert.NoError(t, err)
			}
			mockClient.AssertExpectations(t)
		})
	}
}

func TestSetupTrainingOutputVolume(t *testing.T) {
	testcases := map[string]struct {
		configMap         *corev1.ConfigMap
		expectedOutputDir string
	}{
		"Default Output Dir": {
			configMap: &corev1.ConfigMap{
				Data: map[string]string{
					"training_config.yaml": `
training_config:
  TrainingArguments:
    output_dir: ""
`,
				},
			},
			expectedOutputDir: DefaultOutputVolumePath,
		},
		"Valid Custom Output Dir": {
			configMap: &corev1.ConfigMap{
				Data: map[string]string{
					"training_config.yaml": `
training_config:
  TrainingArguments:
    output_dir: "custom/path"
`,
				},
			},
			expectedOutputDir: "/mnt/custom/path",
		},
		"Output Dir already includes /mnt": {
			configMap: &corev1.ConfigMap{
				Data: map[string]string{
					"training_config.yaml": `
training_config:
  TrainingArguments:
    output_dir: "/mnt/output"
`,
				},
			},
			expectedOutputDir: DefaultOutputVolumePath,
		},
		"Invalid Output Dir": {
			configMap: &corev1.ConfigMap{
				Data: map[string]string{
					"training_config.yaml": `
training_config:
  TrainingArguments:
    output_dir: "../../etc/passwd"
`,
				},
			},
			expectedOutputDir: DefaultOutputVolumePath,
		},
		"No Output Dir Specified": {
			configMap: &corev1.ConfigMap{
				Data: map[string]string{
					"training_config.yaml": `
training_config:
  TrainingArguments: {}
`,
				},
			},
			expectedOutputDir: DefaultOutputVolumePath,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			_, _, resultOutputDir := SetupTrainingOutputVolume(context.Background(), tc.configMap)
			assert.Equal(t, tc.expectedOutputDir, resultOutputDir)
		})
	}
}

func TestHandleImageDataSource(t *testing.T) {
	testcases := map[string]struct {
		workspaceObj              *kaitov1alpha1.Workspace
		expectedInitContainerName string
		expectedVolumeName        string
		expectedVolumeMountPath   string
	}{
		"Handle Image Data Source": {
			workspaceObj: &kaitov1alpha1.Workspace{
				Resource: kaitov1alpha1.ResourceSpec{
					Count: pointer.Int(1),
				},
				Tuning: &kaitov1alpha1.TuningSpec{
					Input: &kaitov1alpha1.DataSource{
						Image: "data-image",
					},
				},
			},
			expectedInitContainerName: "data-extractor",
			expectedVolumeName:        "data-volume",
			expectedVolumeMountPath:   "/mnt/data",
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			initContainer, volume, volumeMount := handleImageDataSource(context.Background(), tc.workspaceObj.Tuning.Input.Image)

			assert.Equal(t, tc.expectedInitContainerName, initContainer.Name)
			assert.Equal(t, tc.workspaceObj.Tuning.Input.Image, initContainer.Image)
			assert.Contains(t, initContainer.Command[2], "cp -r /data/* /mnt/data")

			assert.Equal(t, tc.expectedVolumeName, volume.Name)

			assert.Equal(t, tc.expectedVolumeMountPath, volumeMount.MountPath)
		})
	}
}

func TestHandleURLDataSource(t *testing.T) {
	testcases := map[string]struct {
		workspaceObj              *kaitov1alpha1.Workspace
		expectedInitContainerName string
		expectedImage             string
		expectedCommands          string
		expectedVolumeName        string
		expectedVolumeMountPath   string
	}{
		"Handle URL Data Source": {
			workspaceObj: &kaitov1alpha1.Workspace{
				Tuning: &kaitov1alpha1.TuningSpec{
					Input: &kaitov1alpha1.DataSource{
						URLs: []string{"http://example.com/data1.zip", "http://example.com/data2.zip"},
					},
				},
			},
			expectedInitContainerName: "data-downloader",
			expectedImage:             "curlimages/curl",
			expectedCommands:          "curl -sSL -w \"%{http_code}\" -o \"$DATA_VOLUME_PATH/$filename\" \"$url\"",
			expectedVolumeName:        "data-volume",
			expectedVolumeMountPath:   utils.DefaultDataVolumePath,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			initContainer, volume, volumeMount := handleURLDataSource(context.Background(), tc.workspaceObj)

			assert.Equal(t, tc.expectedInitContainerName, initContainer.Name)
			assert.Equal(t, tc.expectedImage, initContainer.Image)
			assert.Contains(t, normalize(initContainer.Command[2]), normalize(tc.expectedCommands))

			assert.Equal(t, tc.expectedVolumeName, volume.Name)

			assert.Equal(t, tc.expectedVolumeMountPath, volumeMount.MountPath)
		})
	}
}

func TestPrepareTuningParameters(t *testing.T) {
	ctx := context.TODO()

	testcases := map[string]struct {
		name                 string
		workspaceObj         *kaitov1alpha1.Workspace
		modelCommand         string
		tuningObj            *model.PresetParam
		expectedCommands     []string
		expectedRequirements corev1.ResourceRequirements
	}{
		"Basic Tuning Parameters Setup": {
			workspaceObj: &kaitov1alpha1.Workspace{
				Resource: kaitov1alpha1.ResourceSpec{
					InstanceType: "gpu-instance-type",
				},
			},
			modelCommand: "model-command",
			tuningObj: &model.PresetParam{
				RuntimeParam: model.RuntimeParam{
					Transformers: model.HuggingfaceTransformersParam{
						BaseCommand:        "python train.py",
						TorchRunParams:     map[string]string{},
						TorchRunRdzvParams: map[string]string{},
					},
				},
				GPUCountRequirement: "2",
			},
			expectedCommands: []string{"/bin/sh", "-c", "python train.py --num_processes=1 model-command"},
			expectedRequirements: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
				},
			},
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			commands, resources := prepareTuningParameters(ctx, tc.workspaceObj, tc.modelCommand, tc.tuningObj, "2")
			assert.Equal(t, tc.expectedCommands, commands)
			assert.Equal(t, tc.expectedRequirements.Requests, resources.Requests)
			assert.Equal(t, tc.expectedRequirements.Limits, resources.Limits)
		})
	}
}

func TestPrepareDataSource_ImageSource(t *testing.T) {
	ctx := context.TODO()

	workspaceObj := &kaitov1alpha1.Workspace{
		Tuning: &kaitov1alpha1.TuningSpec{
			Input: &kaitov1alpha1.DataSource{
				Image: "custom/data-loader-image",
			},
		},
	}

	// Expected outputs from mocked functions
	expectedVolume := corev1.Volume{
		Name: "data-volume",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{}, // Assume we expect an EmptyDir
		},
	}

	expectedVolumeMount := corev1.VolumeMount{Name: "data-volume", MountPath: "/mnt/data"}
	expectedImagePullSecrets := []corev1.LocalObjectReference{}
	expectedInitContainer := &corev1.Container{
		Name:         "data-extractor",
		Image:        "custom/data-loader-image",
		Command:      []string{"sh", "-c", "ls -la /data && cp -r /data/* /mnt/data && ls -la /mnt/data"},
		VolumeMounts: []corev1.VolumeMount{expectedVolumeMount},
	}

	initContainer, imagePullSecrets, volume, volumeMount, err := prepareDataSource(ctx, workspaceObj)

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, expectedInitContainer, initContainer)
	assert.Equal(t, expectedVolume, volume)
	assert.Equal(t, expectedVolumeMount, volumeMount)
	assert.Equal(t, expectedImagePullSecrets, imagePullSecrets)
}
