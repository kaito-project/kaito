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

package tuning

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/workspace/image"
)

func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
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
			_, _, resultOutputDir := SetupTrainingOutputVolume(context.Background(), tc.configMap, nil)
			assert.Equal(t, tc.expectedOutputDir, resultOutputDir)
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

func TestPrepareDataSource_ImageSource(t *testing.T) {
	ctx := context.TODO()

	workspaceObj := &kaitov1beta1.Workspace{
		Tuning: &kaitov1beta1.TuningSpec{
			Input: &kaitov1beta1.DataSource{
				Name:  "some-name",
				Image: "custom/data-loader-image",
				ImagePullSecrets: []string{
					"image-pull-secret-name",
				},
			},
		},
	}

	// Expected outputs from mocked functions
	expectedInitContainer := image.NewPullerContainer(workspaceObj.Tuning.Input.Image, "/mnt/data")

	expectedVolumes := []corev1.Volume{
		{
			Name: "docker-config-some-name-tuning-input",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "image-pull-secret-name",
								},
								Items: []corev1.KeyToPath{
									{
										Key:  ".dockerconfigjson",
										Path: "image-pull-secret-name.json",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			Name: "data-volume",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	expectedVolumeMounts := []corev1.VolumeMount{
		{
			Name:      "docker-config-some-name-tuning-input",
			MountPath: "/root/.docker/config.d/some-name-tuning-input",
		},
		{
			Name:      "data-volume",
			MountPath: "/mnt/data",
		},
	}
	expectedInitContainer.VolumeMounts = expectedVolumeMounts

	initContainer, volumes, volumeMounts := prepareDataSource(ctx, workspaceObj)

	// Assertions
	assert.Equal(t, expectedInitContainer, initContainer)
	assert.Equal(t, expectedVolumes, volumes)
	assert.Equal(t, expectedVolumeMounts, volumeMounts)
}

func TestSetTrainingResultVolume(t *testing.T) {
	ctx := context.Background()

	testcases := map[string]struct {
		workspace      *kaitov1beta1.Workspace
		existingConfig *corev1.ConfigMap
		expectedError  bool
		expectedVolume string
		expectedMount  string
	}{
		"LoRA config success": {
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Method: kaitov1beta1.TuningMethodLora,
					Config: "lora-config",
					Output: &kaitov1beta1.DataDestination{
						Volume: &corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "output-pvc",
							},
						},
					},
				},
			},
			existingConfig: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lora-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"training_config.yaml": `
training_config:
  TrainingArguments:
    output_dir: "lora-output"
`,
				},
			},
			expectedError:  false,
			expectedVolume: "results-volume",
			expectedMount:  "/mnt/lora-output",
		},
		"QLoRA config success": {
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Method: kaitov1beta1.TuningMethodQLora,
					Config: "qlora-config",
					Output: &kaitov1beta1.DataDestination{
						Volume: &corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
			existingConfig: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "qlora-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"training_config.yaml": `
training_config:
  TrainingArguments:
    output_dir: "qlora-output"
`,
				},
			},
			expectedError:  false,
			expectedVolume: "results-volume",
			expectedMount:  "/mnt/qlora-output",
		},
		"Config not found": {
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Method: kaitov1beta1.TuningMethodLora,
					Config: "missing-config",
					Output: &kaitov1beta1.DataDestination{
						Volume: &corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
			existingConfig: nil,
			expectedError:  true,
		},
		"Invalid config data": {
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Method: kaitov1beta1.TuningMethodLora,
					Config: "invalid-config",
					Output: &kaitov1beta1.DataDestination{
						Volume: &corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
			existingConfig: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"training_config.yaml": "invalid yaml data",
				},
			},
			expectedError: true,
		},
		"Default output directory": {
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Method: kaitov1beta1.TuningMethodLora,
					Config: "default-output-config",
					Output: &kaitov1beta1.DataDestination{
						Volume: &corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
			existingConfig: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default-output-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"training_config.yaml": `
training_config:
  TrainingArguments: {}
`,
				},
			},
			expectedError:  false,
			expectedVolume: "results-volume",
			expectedMount:  DefaultOutputVolumePath,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			// Create a fake client
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = kaitov1beta1.AddToScheme(scheme)

			var kubeClient client.Client
			if tc.existingConfig != nil {
				kubeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(tc.existingConfig).
					Build()
			} else {
				kubeClient = fake.NewClientBuilder().
					WithScheme(scheme).
					Build()
			}

			// Create generator context
			gctx := &generator.WorkspaceGeneratorContext{
				Ctx:        ctx,
				Workspace:  tc.workspace,
				KubeClient: kubeClient,
			}

			// Create a pod spec with a container
			podSpec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: tc.workspace.Name,
					},
				},
			}

			// Call the function
			err := SetTrainingResultVolume(gctx, podSpec)

			// Check error
			if tc.expectedError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Check volume was added
			volumeFound := false
			for _, v := range podSpec.Volumes {
				if v.Name == tc.expectedVolume {
					volumeFound = true
					break
				}
			}
			assert.True(t, volumeFound, "Expected volume %s not found", tc.expectedVolume)

			// Check volume mount was added to the correct container
			mountFound := false
			for _, c := range podSpec.Containers {
				if c.Name == tc.workspace.Name {
					for _, vm := range c.VolumeMounts {
						if vm.Name == tc.expectedVolume && vm.MountPath == tc.expectedMount {
							mountFound = true
							break
						}
					}
				}
			}
			assert.True(t, mountFound, "Expected volume mount not found on container")
		})
	}
}

func TestPrepareTrainingOutput(t *testing.T) {
	ctx := context.Background()

	testcases := map[string]struct {
		workspace          *kaitov1beta1.Workspace
		initialPodSpec     *corev1.PodSpec
		expectedError      bool
		expectedContainers int
		validateFunc       func(*testing.T, *corev1.PodSpec)
	}{
		"Output volume specified - early return": {
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Output: &kaitov1beta1.DataDestination{
						Volume: &corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
			initialPodSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "test-workspace",
					},
				},
			},
			expectedError:      false,
			expectedContainers: 1, // No additional containers added
		},
		"No output image - early return": {
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Output: &kaitov1beta1.DataDestination{
						Image: "",
					},
				},
			},
			initialPodSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "test-workspace",
					},
				},
			},
			expectedError:      false,
			expectedContainers: 1, // No additional containers added
		},
		"Image output with preset": {
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "phi-3-mini-128k-instruct",
						},
					},
					Output: &kaitov1beta1.DataDestination{
						Image:           "registry.example.com/output:latest",
						ImagePushSecret: "push-secret",
					},
				},
			},
			initialPodSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "test-workspace",
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "results-volume",
								MountPath: "/mnt/output",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "results-volume",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
			expectedError:      false,
			expectedContainers: 3, // Original + pusher + pause
			validateFunc: func(t *testing.T, spec *corev1.PodSpec) {
				// Check image pull secrets
				assert.Equal(t, 1, len(spec.ImagePullSecrets))
				assert.Equal(t, "push-secret", spec.ImagePullSecrets[0].Name)

				// Check docker config volume
				volumeFound := false
				for _, v := range spec.Volumes {
					if v.Name == "docker-config" {
						volumeFound = true
						break
					}
				}
				assert.True(t, volumeFound, "docker-config volume not found")

				// Check pusher container
				pusherFound := false
				for _, c := range spec.Containers {
					if c.Name == "pusher" {
						pusherFound = true
						// Verify pusher has the docker config volume mount
						mountFound := false
						for _, vm := range c.VolumeMounts {
							if vm.Name == "docker-config" {
								mountFound = true
								break
							}
						}
						assert.True(t, mountFound, "docker-config volume mount not found in pusher container")
					}
				}
				assert.True(t, pusherFound, "pusher container not found")

				// Check pause container
				pauseFound := false
				for _, c := range spec.Containers {
					if c.Name == "pause" {
						pauseFound = true
					}
				}
				assert.True(t, pauseFound, "pause container not found")
			},
		},
		"Missing results volume mount": {
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Output: &kaitov1beta1.DataDestination{
						Image:           "registry.example.com/output:latest",
						ImagePushSecret: "push-secret",
					},
				},
			},
			initialPodSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "test-workspace",
						// No volume mounts
					},
				},
			},
			expectedError: true,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			// Create generator context
			gctx := &generator.WorkspaceGeneratorContext{
				Ctx:       ctx,
				Workspace: tc.workspace,
			}

			// Deep copy the initial pod spec to avoid mutations
			podSpec := tc.initialPodSpec.DeepCopy()

			// Call the function
			err := SetTrainingOutputImagePush(gctx, podSpec)

			// Check error
			if tc.expectedError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Check number of containers
			assert.Equal(t, tc.expectedContainers, len(podSpec.Containers))

			// Run additional validation if provided
			if tc.validateFunc != nil {
				tc.validateFunc(t, podSpec)
			}
		})
	}
}
