// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package tuning

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestGetInstanceGPUCount(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

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
	testcases := map[string]struct {
		registryName string
		wObj         *kaitov1beta1.Workspace
		presetObj    *model.PresetParam
		expected     string
	}{
		"Valid Registry and Parameters": {
			registryName: "testregistry",
			wObj: &kaitov1beta1.Workspace{
				Tuning: &kaitov1beta1.TuningSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
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
			wObj: &kaitov1beta1.Workspace{
				Tuning: &kaitov1beta1.TuningSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
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
			t.Setenv("PRESET_REGISTRY_NAME", tc.registryName)

			result, _ := GetTuningImageInfo(context.Background(), tc.wObj, tc.presetObj)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetDataSrcImageInfo(t *testing.T) {
	testcases := map[string]struct {
		wObj            *kaitov1beta1.Workspace
		expectedImage   string
		expectedSecrets []corev1.LocalObjectReference
	}{
		"Multiple Image Pull Secrets": {
			wObj: &kaitov1beta1.Workspace{
				Tuning: &kaitov1beta1.TuningSpec{
					Input: &kaitov1beta1.DataSource{
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
			wObj: &kaitov1beta1.Workspace{
				Tuning: &kaitov1beta1.TuningSpec{
					Input: &kaitov1beta1.DataSource{
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
		workspaceObj              *kaitov1beta1.Workspace
		expectedInitContainerName string
		expectedVolumeName        string
		expectedVolumeMountPath   string
	}{
		"Handle Image Data Source": {
			workspaceObj: &kaitov1beta1.Workspace{
				Resource: kaitov1beta1.ResourceSpec{
					Count: ptr.To(1),
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Input: &kaitov1beta1.DataSource{
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
		workspaceObj              *kaitov1beta1.Workspace
		expectedInitContainerName string
		expectedImage             string
		expectedCommands          string
		expectedVolumeName        string
		expectedVolumeMountPath   string
	}{
		"Handle URL Data Source": {
			workspaceObj: &kaitov1beta1.Workspace{
				Tuning: &kaitov1beta1.TuningSpec{
					Input: &kaitov1beta1.DataSource{
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
		workspaceObj         *kaitov1beta1.Workspace
		modelCommand         string
		tuningObj            *model.PresetParam
		expectedCommands     []string
		expectedRequirements corev1.ResourceRequirements
	}{
		"Basic Tuning Parameters Setup": {
			workspaceObj: &kaitov1beta1.Workspace{
				Resource: kaitov1beta1.ResourceSpec{
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
			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

			commands, resources := prepareTuningParameters(ctx, tc.workspaceObj, tc.modelCommand, tc.tuningObj, "2")
			assert.Equal(t, tc.expectedCommands, commands)
			assert.Equal(t, tc.expectedRequirements.Requests, resources.Requests)
			assert.Equal(t, tc.expectedRequirements.Limits, resources.Limits)
		})
	}
}

func TestPrepareDataSource_ImageSource(t *testing.T) {
	ctx := context.TODO()

	workspaceObj := &kaitov1beta1.Workspace{
		Tuning: &kaitov1beta1.TuningSpec{
			Input: &kaitov1beta1.DataSource{
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
