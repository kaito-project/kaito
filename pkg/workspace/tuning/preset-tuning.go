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
	"k8s.io/utils/ptr"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
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
	gpuConfigs := skuHandler.GetGPUConfigs()

	gpuConfig, exists := gpuConfigs[sku]
	if !exists {
		return 1
	}
	return gpuConfig.GPUCount
}

func GetTuningImageInfo(ctx context.Context, workspaceObj *kaitov1alpha1.Workspace, presetObj *model.PresetParam) (string, []corev1.LocalObjectReference) {
	imagePullSecretRefs := []corev1.LocalObjectReference{}
	// Check if the workspace preset's access mode is private
	if string(workspaceObj.Tuning.Preset.AccessMode) == string(kaitov1alpha1.ModelImageAccessModePrivate) {
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

func GetDataSrcImageInfo(ctx context.Context, wObj *kaitov1alpha1.Workspace) (string, []corev1.LocalObjectReference) {
	imagePullSecretRefs := make([]corev1.LocalObjectReference, len(wObj.Tuning.Input.ImagePullSecrets))
	for i, secretName := range wObj.Tuning.Input.ImagePullSecrets {
		imagePullSecretRefs[i] = corev1.LocalObjectReference{Name: secretName}
	}
	return wObj.Tuning.Input.Image, imagePullSecretRefs
}

func dockerSidecarScriptPushImage(outputDir, image string) string {
	return fmt.Sprintf(`
# Start the Docker daemon in the background with specific options for DinD
dockerd &
# Wait for the Docker daemon to be ready
while ! docker info > /dev/null 2>&1; do
  echo "Waiting for Docker daemon to start..."
  sleep 1
done
echo 'Docker daemon started'

PUSH_SUCCEEDED=false

while true; do
  FILE_PATH=$(find %s -name 'fine_tuning_completed.txt')
  if [ ! -z "$FILE_PATH" ]; then
    if [ "$PUSH_SUCCEEDED" = false ]; then
      echo "FOUND TRAINING COMPLETED FILE at $FILE_PATH"

      PARENT_DIR=$(dirname "$FILE_PATH")
      echo "Parent directory is $PARENT_DIR"

      TEMP_CONTEXT=$(mktemp -d)
      cp "$PARENT_DIR/adapter_config.json" "$TEMP_CONTEXT/adapter_config.json"
      cp -r "$PARENT_DIR/adapter_model.safetensors" "$TEMP_CONTEXT/adapter_model.safetensors"

      # Create a minimal Dockerfile
      echo 'FROM busybox:latest
      RUN mkdir -p /data
      ADD adapter_config.json /data/
      ADD adapter_model.safetensors /data/' > "$TEMP_CONTEXT/Dockerfile"

	  # Add symbolic link to read-only mounted config.json
      mkdir -p /root/.docker
	  ln -s /tmp/.docker/config/config.json /root/.docker/config.json

      docker build -t %s "$TEMP_CONTEXT"
      
      while true; do
        if docker push %s; then
          echo "Upload complete"
          # Cleanup: Remove the temporary directory
          rm -rf "$TEMP_CONTEXT"
          # Remove the file to prevent repeated builds
          rm "$FILE_PATH"
          PUSH_SUCCEEDED=true
          # Signal completion
          touch /tmp/upload_complete
          exit 0
        else
          echo "Push failed, retrying in 30 seconds..."
          sleep 30
        fi
      done
    fi
  fi
  sleep 10  # Check every 10 seconds
done`, outputDir, image, image)
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
	config, err := kaitov1alpha1.UnmarshalTrainingConfig(configMap)
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

func setupDefaultSharedVolumes(workspaceObj *kaitov1alpha1.Workspace, cmName string) ([]corev1.Volume, []corev1.VolumeMount) {
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

func CreatePresetTuning(ctx context.Context, workspaceObj *kaitov1alpha1.Workspace, revisionNum string,
	tuningObj *model.PresetParam, kubeClient client.Client) (client.Object, error) {

	var defaultConfigName string
	if workspaceObj.Tuning.Method == kaitov1alpha1.TuningMethodLora {
		defaultConfigName = kaitov1alpha1.DefaultLoraConfigMapTemplate
	} else if workspaceObj.Tuning.Method == kaitov1alpha1.TuningMethodQLora {
		defaultConfigName = kaitov1alpha1.DefaultQloraConfigMapTemplate
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

	var initContainers, sidecarContainers []corev1.Container
	volumes, volumeMounts := setupDefaultSharedVolumes(workspaceObj, configVolume.Name)

	// Add shared volume for training output
	trainingOutputVolume, trainingOutputVolumeMount, outputDir := SetupTrainingOutputVolume(ctx, configVolume)
	volumes = append(volumes, trainingOutputVolume)
	volumeMounts = append(volumeMounts, trainingOutputVolumeMount)

	initContainer, imagePullSecrets, dataSourceVolume, dataSourceVolumeMount, err := prepareDataSource(ctx, workspaceObj)
	if err != nil {
		return nil, err
	}
	volumes = append(volumes, dataSourceVolume)
	volumeMounts = append(volumeMounts, dataSourceVolumeMount)
	if initContainer.Name != "" {
		initContainers = append(initContainers, *initContainer)
	}

	sidecarContainer, imagePushSecret, dataDestVolume, dataDestVolumeMount, err := prepareDataDestination(ctx, workspaceObj, outputDir)
	if err != nil {
		return nil, err
	}
	volumes = append(volumes, dataDestVolume)
	volumeMounts = append(volumeMounts, dataDestVolumeMount)
	if sidecarContainer != nil {
		sidecarContainers = append(sidecarContainers, *sidecarContainer)
	}
	if imagePushSecret != nil {
		imagePullSecrets = append(imagePullSecrets, *imagePushSecret)
	}

	modelCommand, err := prepareModelRunParameters(ctx, tuningObj)
	if err != nil {
		return nil, err
	}

	skuNumGPUs, err := utils.GetSKUNumGPUs(ctx, kubeClient, workspaceObj.Status.WorkerNodes,
		workspaceObj.Resource.InstanceType, tuningObj.GPUCountRequirement)
	if err != nil {
		return nil, fmt.Errorf("failed to get SKU num GPUs: %v", err)
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
func prepareDataDestination(ctx context.Context, workspaceObj *kaitov1alpha1.Workspace, outputDir string) (*corev1.Container, *corev1.LocalObjectReference, corev1.Volume, corev1.VolumeMount, error) {
	var sidecarContainer *corev1.Container
	var volume corev1.Volume
	var volumeMount corev1.VolumeMount
	var imagePushSecret *corev1.LocalObjectReference
	switch {
	case workspaceObj.Tuning.Output.Image != "":
		image, secret := workspaceObj.Tuning.Output.Image, workspaceObj.Tuning.Output.ImagePushSecret
		imagePushSecret = &corev1.LocalObjectReference{Name: secret}
		sidecarContainer, volume, volumeMount = handleImageDataDestination(ctx, outputDir, image, secret)
		// TODO: Future PR include
		//case workspaceObj.Tuning.Output.Volume != nil:
	}
	return sidecarContainer, imagePushSecret, volume, volumeMount, nil
}

func handleImageDataDestination(ctx context.Context, outputDir, image, imagePushSecret string) (*corev1.Container, corev1.Volume, corev1.VolumeMount) {
	sidecarContainer := &corev1.Container{
		Name:  "docker-sidecar",
		Image: "docker:dind",
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(true),
		},
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{dockerSidecarScriptPushImage(outputDir, image)},
	}
	volume, volumeMount := utils.ConfigImagePushSecretVolume(imagePushSecret)
	return sidecarContainer, volume, volumeMount
}

// Now there are three options for DataSource: 1. URL - 2. HostPath - 3. Image
func prepareDataSource(ctx context.Context, workspaceObj *kaitov1alpha1.Workspace) (*corev1.Container, []corev1.LocalObjectReference, corev1.Volume, corev1.VolumeMount, error) {
	var initContainer *corev1.Container
	var volume corev1.Volume
	var volumeMount corev1.VolumeMount
	var imagePullSecrets []corev1.LocalObjectReference
	switch {
	case workspaceObj.Tuning.Input.Image != "":
		var image string
		image, imagePullSecrets = GetDataSrcImageInfo(ctx, workspaceObj)
		initContainer, volume, volumeMount = handleImageDataSource(ctx, image)
	case len(workspaceObj.Tuning.Input.URLs) > 0:
		initContainer, volume, volumeMount = handleURLDataSource(ctx, workspaceObj)
		// TODO: Future PR include
		// case workspaceObj.Tuning.Input.Volume != nil:
	}
	return initContainer, imagePullSecrets, volume, volumeMount, nil
}

func handleImageDataSource(ctx context.Context, image string) (*corev1.Container, corev1.Volume, corev1.VolumeMount) {
	// Constructing a multistep command that lists, copies, and then lists the destination
	command := "ls -la /data && cp -r /data/* " + utils.DefaultDataVolumePath + " && ls -la " + utils.DefaultDataVolumePath
	initContainer := &corev1.Container{
		Name:    "data-extractor",
		Image:   image,
		Command: []string{"sh", "-c", command},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "data-volume",
				MountPath: utils.DefaultDataVolumePath,
			},
		},
	}

	volume, volumeMount := utils.ConfigDataVolume(nil)
	return initContainer, volume, volumeMount
}

func handleURLDataSource(ctx context.Context, workspaceObj *kaitov1alpha1.Workspace) (*corev1.Container, corev1.Volume, corev1.VolumeMount) {
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
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "data-volume",
				MountPath: utils.DefaultDataVolumePath,
			},
		},
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
func prepareTuningParameters(ctx context.Context, wObj *kaitov1alpha1.Workspace, modelCommand string,
	tuningObj *model.PresetParam, skuNumGPUs string) ([]string, corev1.ResourceRequirements) {
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
			corev1.ResourceName(resources.CapacityNvidiaGPU): resource.MustParse(skuNumGPUs),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceName(resources.CapacityNvidiaGPU): resource.MustParse(skuNumGPUs),
		},
	}

	return commands, resourceRequirements
}
