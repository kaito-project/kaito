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
	"fmt"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/image"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

const (
	DefaultBaseDir          = "/mnt"
	DefaultOutputVolumePath = "/mnt/output"
)

var (
	containerPorts = []corev1.ContainerPort{{
		ContainerPort: consts.PortInferenceServer,
	}}

	// Come up with valid liveness and readiness probes for fine-tuning
	// TODO: livenessProbe = &corev1.Probe{}
	// TODO: readinessProbe = &corev1.Probe{}

	tolerations = []corev1.Toleration{
		{
			Effect:   corev1.TaintEffectNoSchedule,
			Operator: corev1.TolerationOpEqual,
			Key:      consts.GPUString,
		},
		{
			Effect: corev1.TaintEffectNoSchedule,
			Value:  consts.GPUString,
			Key:    consts.SKUString,
		},
	}
)

func GetTuningImageInfo() string {
	presetObj := metadata.MustGet("base")
	return utils.GetPresetImageName(presetObj.Name, presetObj.Tag)
}

func GetDataSrcImageInfo(ctx context.Context, wObj *kaitov1beta1.Workspace) (string, []corev1.LocalObjectReference) {
	imagePullSecretRefs := make([]corev1.LocalObjectReference, len(wObj.Tuning.Input.ImagePullSecrets))
	for i, secretName := range wObj.Tuning.Input.ImagePullSecrets {
		imagePullSecretRefs[i] = corev1.LocalObjectReference{Name: secretName}
	}
	return wObj.Tuning.Input.Image, imagePullSecretRefs
}

// PrepareOutputDir ensures the output directory is within the base directory.
func PrepareOutputDir(outputDir string) (string, error) {
	if outputDir == "" {
		return DefaultOutputVolumePath, nil
	}
	cleanPath := outputDir
	if !strings.HasPrefix(cleanPath, DefaultBaseDir) {
		cleanPath = filepath.Join(DefaultBaseDir, outputDir)
	}
	cleanPath = filepath.Clean(cleanPath)
	if cleanPath == DefaultBaseDir || !strings.HasPrefix(cleanPath, DefaultBaseDir) {
		klog.InfoS("Invalid output_dir specified: '%s', must be a directory. Using default output_dir: %s", outputDir, DefaultOutputVolumePath)
		return DefaultOutputVolumePath, fmt.Errorf("invalid output_dir specified: '%s', must be a directory", outputDir)
	}
	return cleanPath, nil
}

// GetOutputDirFromTrainingArgs retrieves the output directory from training arguments if specified.
func GetOutputDirFromTrainingArgs(trainingArgs map[string]runtime.RawExtension) (string, *apis.FieldError) {
	if trainingArgsRaw, exists := trainingArgs["TrainingArguments"]; exists {
		outputDirValue, found, err := utils.SearchRawExtension(trainingArgsRaw, "output_dir")
		if err != nil {
			return "", apis.ErrGeneric(fmt.Sprintf("Failed to parse 'output_dir': %v", err), "output_dir")
		}
		if found {
			outputDir, ok := outputDirValue.(string)
			if !ok {
				return "", apis.ErrInvalidValue("output_dir is not a string", "output_dir")
			}
			return outputDir, nil
		}
	}
	return "", nil
}

// GetTrainingOutputDir retrieves and validates the output directory from the ConfigMap.
func GetTrainingOutputDir(ctx context.Context, configMap *corev1.ConfigMap) (string, error) {
	config, err := kaitov1beta1.UnmarshalTrainingConfig(configMap)
	if err != nil {
		return "", err
	}

	outputDir := ""
	if trainingArgs := config.TrainingConfig.TrainingArguments; trainingArgs != nil {
		outputDir, err = GetOutputDirFromTrainingArgs(trainingArgs)
		if err != nil {
			return "", err
		}
	}

	return PrepareOutputDir(outputDir)
}

// SetupTrainingOutputVolume adds shared volume for results dir
func SetupTrainingOutputVolume(ctx context.Context, configMap *corev1.ConfigMap, outputVolume *corev1.VolumeSource) (corev1.Volume, corev1.VolumeMount, string) {
	outputDir, _ := GetTrainingOutputDir(ctx, configMap)
	resultsVolume, resultsVolumeMount := utils.ConfigResultsVolume(outputDir, outputVolume)
	return resultsVolume, resultsVolumeMount, outputDir
}

func CreatePresetTuning(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, revisionNum string,
	model pkgmodel.Model, kubeClient client.Client) (client.Object, error) {

	var skuNumGPUs int
	gpuConfig, err := utils.GetGPUConfigBySKU(workspaceObj.Resource.InstanceType)
	if err != nil {
		gpuConfig, err = utils.TryGetGPUConfigFromNode(ctx, kubeClient, workspaceObj.Status.WorkerNodes)
		if err != nil {
			defaultNumGPU := resource.MustParse(model.GetTuningParameters().GPUCountRequirement)
			skuNumGPUs = int(defaultNumGPU.Value())
		}
	}
	if gpuConfig != nil {
		skuNumGPUs = gpuConfig.GPUCount
	}

	gctx := &generator.WorkspaceGeneratorContext{
		Ctx:        ctx,
		Workspace:  workspaceObj,
		Model:      model,
		KubeClient: kubeClient,
	}

	podSpec, err := generator.GenerateManifest(gctx,
		GenerateBasicTuningPodSpec(gpuConfig, skuNumGPUs),
		SetTrainingResultVolume,
		SetTrainingInput,
		SetTrainingOutputImagePush,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate pod spec: %w", err)
	}

	jobObj, err := generator.GenerateManifest(gctx,
		manifests.GenerateTuningJobManifest(revisionNum),
		manifests.SetJobPodSpec(podSpec),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate job manifest: %w", err)
	}

	err = resources.CreateResource(ctx, jobObj, kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return jobObj, nil
}

func GenerateBasicTuningPodSpec(gpuConfig *sku.GPUConfig, skuNumGPUs int) func(*generator.WorkspaceGeneratorContext, *corev1.PodSpec) error {
	return func(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
		// additional volume
		var volumes []corev1.Volume
		var volumeMounts []corev1.VolumeMount
		var initContainers []corev1.Container

		// add share memory for cross process communication
		shmVolume, shmVolumeMount := utils.ConfigSHMVolume()
		volumes = append(volumes, shmVolume)
		volumeMounts = append(volumeMounts, shmVolumeMount)

		// Add volume for model weights access
		volumes = append(volumes, utils.DefaultModelWeightsVolume)
		volumeMounts = append(volumeMounts, utils.DefaultModelWeightsVolumeMount)
		initContainers = append(initContainers, manifests.GenerateModelPullerContainer(ctx.Ctx, ctx.Workspace, ctx.Model.GetTuningParameters())...)

		// resource requirements
		resourceRequirements := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName(resources.CapacityNvidiaGPU): *resource.NewQuantity(int64(skuNumGPUs), resource.DecimalSI),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceName(resources.CapacityNvidiaGPU): *resource.NewQuantity(int64(skuNumGPUs), resource.DecimalSI),
			},
		}

		// default env
		var envVars []corev1.EnvVar

		// Append environment variable for default target modules if using Phi3 model
		presetName := strings.ToLower(string(ctx.Workspace.Tuning.Preset.Name))
		if strings.HasPrefix(presetName, "phi-3") {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "DEFAULT_TARGET_MODULES",
				Value: "k_proj,q_proj,v_proj,o_proj,gate_proj,down_proj,up_proj",
			})
		}
		// Add Expandable Memory Feature to reduce Peak GPU Mem Usage
		envVars = append(envVars, corev1.EnvVar{
			Name:  "PYTORCH_CUDA_ALLOC_CONF",
			Value: "expandable_segments:True",
		})

		// tuning commands
		tuningParam := ctx.Model.GetInferenceParameters().DeepCopy()
		commands := tuningParam.GetTuningCommand(pkgmodel.RuntimeContext{
			SKUNumGPUs: skuNumGPUs,
		})

		spec.Tolerations = tolerations
		spec.InitContainers = append(spec.InitContainers, initContainers...)
		spec.Containers = []corev1.Container{
			{
				Name:         ctx.Workspace.Name,
				Image:        GetTuningImageInfo(),
				Command:      commands,
				Resources:    resourceRequirements,
				Ports:        containerPorts,
				Env:          envVars,
				VolumeMounts: volumeMounts,
			},
		}
		spec.Volumes = volumes
		spec.RestartPolicy = corev1.RestartPolicyNever
		return nil
	}
}

func SetTrainingResultVolume(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	var defaultConfigName string
	if ctx.Workspace.Tuning.Method == kaitov1beta1.TuningMethodLora {
		defaultConfigName = kaitov1beta1.DefaultLoraConfigMapTemplate
	} else if ctx.Workspace.Tuning.Method == kaitov1beta1.TuningMethodQLora {
		defaultConfigName = kaitov1beta1.DefaultQloraConfigMapTemplate
	}
	configVolume, err := resources.EnsureConfigOrCopyFromDefault(ctx.Ctx, ctx.KubeClient,
		client.ObjectKey{
			Namespace: ctx.Workspace.Namespace,
			Name:      ctx.Workspace.Tuning.Config,
		},
		client.ObjectKey{Name: defaultConfigName},
	)
	if err != nil {
		return err
	}

	// Add shared volume for tuning parameters
	cmVolume, cmVolumeMount := utils.ConfigCMVolume(configVolume.Name)

	// Add results volume for training output
	outputDir, err := GetTrainingOutputDir(ctx.Ctx, configVolume)
	if err != nil {
		return fmt.Errorf("failed to get training output directory from config: %w", err)
	}
	resultsVolume, resultsVolumeMount := utils.ConfigResultsVolume(outputDir, ctx.Workspace.Tuning.Output.Volume)

	volumes := []corev1.Volume{cmVolume, resultsVolume}
	volumeMounts := []corev1.VolumeMount{cmVolumeMount, resultsVolumeMount}
	for i := range spec.Containers {
		if spec.Containers[i].Name == ctx.Workspace.Name {
			spec.Containers[i].VolumeMounts = append(spec.Containers[i].VolumeMounts, volumeMounts...)
			spec.Volumes = append(spec.Volumes, volumes...)
			break
		}
	}

	return nil
}

// Now there are two options for data destination 1. Volume - 2. Image
// notes: this modifier requires the results volume to be set in the pod spec
func SetTrainingOutputImagePush(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	tuning := ctx.Workspace.Tuning
	output := tuning.Output

	if output.Volume != nil || output.Image == "" {
		return nil
	}

	var annotationsData map[string]map[string]string
	if preset := tuning.Preset; preset != nil {
		annotationsData = map[string]map[string]string{
			"$manifest": {
				"sh.kaito.model.name": string(preset.Name),
			},
		}
	}

	// additional secret volume for image push
	imagePushSecretRef := corev1.LocalObjectReference{
		Name: output.ImagePushSecret,
	}
	spec.ImagePullSecrets = append(spec.ImagePullSecrets, imagePushSecretRef)
	secretVolume, secretVolumeMount := utils.ConfigImagePushSecretVolume(imagePushSecretRef.Name)
	spec.Volumes = append(spec.Volumes, secretVolume)

	// additional sidecar container for uploading image
	resultVolumeMount := utils.FindResultsVolumeMount(spec)
	if resultVolumeMount == nil {
		return fmt.Errorf("results volume mount not found in pod spec")
	}
	inputDirectory := resultVolumeMount.MountPath
	pusherContainer := image.NewPusherContainer(inputDirectory, output.Image, annotationsData, nil)
	pusherContainer.VolumeMounts = append(pusherContainer.VolumeMounts, secretVolumeMount, *resultVolumeMount)
	pauseContainer := corev1.Container{
		Name:            "pause",
		Image:           "registry.k8s.io/pause:latest",
		ImagePullPolicy: corev1.PullAlways,
	}
	spec.Containers = append(spec.Containers, *pusherContainer, pauseContainer)
	return nil
}

func SetTrainingInput(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	initContainer, dataSourceVolumes, dataSourceVolumeMounts := prepareDataSource(ctx.Ctx, ctx.Workspace)
	if initContainer != nil && initContainer.Name != "" {
		spec.InitContainers = append(spec.InitContainers, *initContainer)
	}

	spec.Volumes = append(spec.Volumes, dataSourceVolumes...)

	for _, volumeMount := range dataSourceVolumeMounts {
		for i := range spec.Containers {
			spec.Containers[i].VolumeMounts = append(spec.Containers[i].VolumeMounts, volumeMount)
		}
	}

	return nil
}

// Now there are three options for DataSource: 1. URL - 2. Volume - 3. Image
func prepareDataSource(ctx context.Context, workspaceObj *kaitov1beta1.Workspace) (*corev1.Container, []corev1.Volume, []corev1.VolumeMount) {
	input := workspaceObj.Tuning.Input

	switch {
	case input.Image != "":
		dataVolume, dataVolumeMount := utils.ConfigDataVolume(nil)
		imagePullSecretVolume, imagePullSecretVolumeMount := utils.ConfigImagePullSecretVolume(input.Name+"-tuning-input", input.ImagePullSecrets)
		pullerContainer := image.NewPullerContainer(input.Image, utils.DefaultDataVolumePath)
		pullerContainer.VolumeMounts = append(pullerContainer.VolumeMounts, imagePullSecretVolumeMount, dataVolumeMount)
		return pullerContainer, []corev1.Volume{imagePullSecretVolume, dataVolume}, []corev1.VolumeMount{imagePullSecretVolumeMount, dataVolumeMount}

	case len(input.URLs) > 0:
		initContainer, volume, volumeMount := handleURLDataSource(ctx, workspaceObj)
		return initContainer, []corev1.Volume{volume}, []corev1.VolumeMount{volumeMount}

	case input.Volume != nil:
		dataVolume, dataVolumeMount := utils.ConfigDataVolume(input.Volume)
		return nil, []corev1.Volume{dataVolume}, []corev1.VolumeMount{dataVolumeMount}

	default:
		return nil, nil, nil
	}
}

func handleURLDataSource(ctx context.Context, workspaceObj *kaitov1beta1.Workspace) (*corev1.Container, corev1.Volume, corev1.VolumeMount) {
	initContainer := &corev1.Container{
		Name:  "data-downloader",
		Image: "curlimages/curl",
		Command: []string{"sh", "-c", `
			if [ -z "$DATA_URLS" ]; then
				echo "No URLs provided in DATA_URLS."
				exit 1
			fi
			for url in $DATA_URLS; do
				filename=$(basename "$url" | sed 's/[?=&]/_/g')
				echo "Downloading $url to $DATA_VOLUME_PATH/$filename"
				retry_count=0
				while [ $retry_count -lt 3 ]; do
					http_status=$(curl -sSL -w "%{http_code}" -o "$DATA_VOLUME_PATH/$filename" "$url")
					curl_exit_status=$?  # Save the exit status of curl immediately
					if [ "$http_status" -eq 200 ] && [ -s "$DATA_VOLUME_PATH/$filename" ] && [ $curl_exit_status -eq 0 ]; then
						echo "Successfully downloaded $url"
						break
					else
						echo "Failed to download $url, HTTP status code: $http_status, retrying..."
						retry_count=$((retry_count + 1))
						rm -f "$DATA_VOLUME_PATH/$filename" # Remove incomplete file
						sleep 2
					fi
				done
				if [ $retry_count -eq 3 ]; then
					echo "Failed to download $url after 3 attempts"
					exit 1  # Exit with a non-zero status to indicate failure
				fi
			done
			echo "All downloads completed successfully"
		`},
		Env: []corev1.EnvVar{
			{
				Name:  "DATA_URLS",
				Value: strings.Join(workspaceObj.Tuning.Input.URLs, " "),
			},
			{
				Name:  "DATA_VOLUME_PATH",
				Value: utils.DefaultDataVolumePath,
			},
		},
	}
	volume, volumeMount := utils.ConfigDataVolume(nil)
	return initContainer, volume, volumeMount
}
