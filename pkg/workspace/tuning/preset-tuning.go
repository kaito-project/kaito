// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package tuning

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/image"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
)

const (
	Port5000                = int32(5000)
	TuningFile              = "/workspace/tfs/fine_tuning.py"
	DefaultBaseDir          = "/mnt"
	DefaultOutputVolumePath = "/mnt/output"
)

var (
	containerPorts = []corev1.ContainerPort{{
		ContainerPort: Port5000,
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

func getInstanceGPUCount(sku string) int {
	skuHandler, _ := utils.GetSKUHandler()
	if gpuConfig := skuHandler.GetGPUConfigBySKU(sku); gpuConfig != nil {
		return gpuConfig.GPUCount
	}
	return 1
}

func GetTuningImageInfo(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, presetObj *model.PresetParam) (string, []corev1.LocalObjectReference) {
	imagePullSecretRefs := []corev1.LocalObjectReference{}
	// Check if the workspace preset's access mode is private
	if string(workspaceObj.Tuning.Preset.AccessMode) == string(kaitov1beta1.ModelImageAccessModePrivate) {
		imageName := workspaceObj.Tuning.Preset.PresetOptions.Image
		for _, secretName := range workspaceObj.Tuning.Preset.PresetOptions.ImagePullSecrets {
			imagePullSecretRefs = append(imagePullSecretRefs, corev1.LocalObjectReference{Name: secretName})
		}
		return imageName, imagePullSecretRefs
	} else {
		imageName := string(workspaceObj.Tuning.Preset.Name)
		imageTag := presetObj.Tag
		registryName := os.Getenv("PRESET_REGISTRY_NAME")
		imageName = fmt.Sprintf("%s/kaito-%s:%s", registryName, imageName, imageTag)
		return imageName, imagePullSecretRefs
	}
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
func SetupTrainingOutputVolume(ctx context.Context, configMap *corev1.ConfigMap) (corev1.Volume, corev1.VolumeMount, string) {
	outputDir, _ := GetTrainingOutputDir(ctx, configMap)
	resultsVolume, resultsVolumeMount := utils.ConfigResultsVolume(outputDir)
	return resultsVolume, resultsVolumeMount, outputDir
}

func setupDefaultSharedVolumes(workspaceObj *kaitov1beta1.Workspace, cmName string) ([]corev1.Volume, []corev1.VolumeMount) {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	// Add shared volume for shared memory (multi-node)
	shmVolume, shmVolumeMount := utils.ConfigSHMVolume(*workspaceObj.Resource.Count)
	if shmVolume.Name != "" {
		volumes = append(volumes, shmVolume)
	}
	if shmVolumeMount.Name != "" {
		volumeMounts = append(volumeMounts, shmVolumeMount)
	}

	// Add shared volume for tuning parameters
	cmVolume, cmVolumeMount := utils.ConfigCMVolume(cmName)
	volumes = append(volumes, cmVolume)
	volumeMounts = append(volumeMounts, cmVolumeMount)

	return volumes, volumeMounts
}

func CreatePresetTuning(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, revisionNum string,
	tuningObj *model.PresetParam, kubeClient client.Client) (client.Object, error) {

	var defaultConfigName string
	if workspaceObj.Tuning.Method == kaitov1beta1.TuningMethodLora {
		defaultConfigName = kaitov1beta1.DefaultLoraConfigMapTemplate
	} else if workspaceObj.Tuning.Method == kaitov1beta1.TuningMethodQLora {
		defaultConfigName = kaitov1beta1.DefaultQloraConfigMapTemplate
	}
	configVolume, err := resources.EnsureConfigOrCopyFromDefault(ctx, kubeClient,
		client.ObjectKey{
			Namespace: workspaceObj.Namespace,
			Name:      workspaceObj.Tuning.Config,
		},
		client.ObjectKey{Name: defaultConfigName},
	)
	if err != nil {
		return nil, err
	}

	var imagePullSecrets []corev1.LocalObjectReference
	var initContainers, sidecarContainers []corev1.Container
	volumes, volumeMounts := setupDefaultSharedVolumes(workspaceObj, configVolume.Name)

	// TODO: make containers only mount the volumes they need

	// Add shared volume for training output
	trainingOutputVolume, trainingOutputVolumeMount, outputDir := SetupTrainingOutputVolume(ctx, configVolume)
	volumes = append(volumes, trainingOutputVolume)
	volumeMounts = append(volumeMounts, trainingOutputVolumeMount)

	initContainer, dataSourceVolumes, dataSourceVolumeMounts := prepareDataSource(ctx, workspaceObj)
	volumes = append(volumes, dataSourceVolumes...)
	volumeMounts = append(volumeMounts, dataSourceVolumeMounts...)
	if initContainer.Name != "" {
		initContainers = append(initContainers, *initContainer)
	}

	sidecarContainer, imagePushSecret, dataDestVolume, dataDestVolumeMount := prepareDataDestination(ctx, workspaceObj, outputDir)
	volumes = append(volumes, dataDestVolume)
	volumeMounts = append(volumeMounts, dataDestVolumeMount)
	if sidecarContainer != nil {
		sidecarContainers = append(sidecarContainers, *sidecarContainer)
	}
	if imagePushSecret != nil {
		imagePullSecrets = append(imagePullSecrets, *imagePushSecret)
	}

	pauseContainer := corev1.Container{
		Name:            "pause",
		Image:           "registry.k8s.io/pause:latest",
		ImagePullPolicy: corev1.PullAlways,
	}
	sidecarContainers = append(sidecarContainers, pauseContainer)

	modelCommand, err := prepareModelRunParameters(ctx, tuningObj)
	if err != nil {
		return nil, err
	}

	var skuNumGPUs int
	gpuConfig, err := utils.GetGPUConfigBySKU(workspaceObj.Resource.InstanceType)
	if err != nil {
		gpuConfig, err = utils.TryGetGPUConfigFromNode(ctx, kubeClient, workspaceObj.Status.WorkerNodes)
		if err != nil {
			defaultNumGPU := resource.MustParse(tuningObj.GPUCountRequirement)
			skuNumGPUs = int(defaultNumGPU.Value())
		}
	}
	if gpuConfig != nil {
		skuNumGPUs = gpuConfig.GPUCount
	}

	commands, resourceReq := prepareTuningParameters(ctx, workspaceObj, modelCommand, tuningObj, skuNumGPUs)
	tuningImage, tuningImagePullSecrets := GetTuningImageInfo(ctx, workspaceObj, tuningObj)
	if tuningImagePullSecrets != nil {
		imagePullSecrets = append(imagePullSecrets, tuningImagePullSecrets...)
	}

	var envVars []corev1.EnvVar
	presetName := strings.ToLower(string(workspaceObj.Tuning.Preset.Name))
	// Append environment variable for default target modules if using Phi3 model
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
	jobObj := manifests.GenerateTuningJobManifest(ctx, workspaceObj, revisionNum, tuningImage, imagePullSecrets, *workspaceObj.Resource.Count, commands,
		containerPorts, nil, nil, resourceReq, tolerations, initContainers, sidecarContainers, volumes, volumeMounts, envVars)

	err = resources.CreateResource(ctx, jobObj, kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return jobObj, nil
}

// Now there are two options for data destination 1. HostPath - 2. Image
func prepareDataDestination(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, inputDirectory string) (*corev1.Container, *corev1.LocalObjectReference, corev1.Volume, corev1.VolumeMount) {
	tuning := workspaceObj.Tuning
	output := tuning.Output

	outputImage := output.Image
	if outputImage == "" {
		return nil, nil, corev1.Volume{}, corev1.VolumeMount{}
	}

	var annotationsData map[string]map[string]string
	if preset := tuning.Preset; preset != nil {
		annotationsData = map[string]map[string]string{
			"$manifest": {
				"sh.kaito.model.name": string(preset.Name),
			},
		}
	}

	pusherContainer := image.NewPusherContainer(inputDirectory, outputImage, annotationsData, nil)

	imagePushSecretRef := corev1.LocalObjectReference{
		Name: output.ImagePushSecret,
	}

	volume, volumeMount := utils.ConfigImagePushSecretVolume(imagePushSecretRef.Name)

	return pusherContainer, &imagePushSecretRef, volume, volumeMount
}

// Now there are three options for DataSource: 1. URL - 2. HostPath - 3. Image
func prepareDataSource(ctx context.Context, workspaceObj *kaitov1beta1.Workspace) (*corev1.Container, []corev1.Volume, []corev1.VolumeMount) {
	input := workspaceObj.Tuning.Input

	switch {
	case input.Image != "":
		dataVolume, dataVolumeMount := utils.ConfigDataVolume(nil)
		imagePullSecretVolume, imagePullSecretVolumeMount := utils.ConfigImagePullSecretVolume(input.Name+"-tuning-input", input.ImagePullSecrets)
		pullerContainer := image.NewPullerContainer(input.Image, utils.DefaultDataVolumePath)
		return pullerContainer, []corev1.Volume{imagePullSecretVolume, dataVolume}, []corev1.VolumeMount{imagePullSecretVolumeMount, dataVolumeMount}

	case len(input.URLs) > 0:
		initContainer, volume, volumeMount := handleURLDataSource(ctx, workspaceObj)
		return initContainer, []corev1.Volume{volume}, []corev1.VolumeMount{volumeMount}

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

func prepareModelRunParameters(ctx context.Context, tuningObj *model.PresetParam) (string, error) {
	modelCommand := utils.BuildCmdStr(TuningFile, tuningObj.Transformers.ModelRunParams)
	return modelCommand, nil
}

// prepareTuningParameters builds a PyTorch command:
// accelerate launch <TORCH_PARAMS> baseCommand <MODEL_PARAMS>
// and sets the GPU resources required for tuning.
// Returns the command and resource configuration.
func prepareTuningParameters(ctx context.Context, wObj *kaitov1beta1.Workspace, modelCommand string,
	tuningObj *model.PresetParam, skuNumGPUs int) ([]string, corev1.ResourceRequirements) {
	hfParam := tuningObj.Transformers // Only support Huggingface for now
	if hfParam.TorchRunParams == nil {
		hfParam.TorchRunParams = make(map[string]string)
	}
	// Set # of processes to GPU Count
	numProcesses := getInstanceGPUCount(wObj.Resource.InstanceType)
	hfParam.TorchRunParams["num_processes"] = fmt.Sprintf("%d", numProcesses)
	torchCommand := utils.BuildCmdStr(hfParam.BaseCommand, hfParam.TorchRunParams, hfParam.TorchRunRdzvParams)
	commands := utils.ShellCmd(torchCommand + " " + modelCommand)

	resourceRequirements := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceName(resources.CapacityNvidiaGPU): *resource.NewQuantity(int64(skuNumGPUs), resource.DecimalSI),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceName(resources.CapacityNvidiaGPU): *resource.NewQuantity(int64(skuNumGPUs), resource.DecimalSI),
		},
	}

	return commands, resourceRequirements
}
