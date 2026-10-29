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

package controllers

import (
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/pkg/utils/workspace"
)

const (
	lmCacheSidecarName             = "lmcache-mp-server"
	lmCacheRuntimeVolumeName       = "lmcache-runtime"
	lmCacheDeploymentModeEnvName   = "KAITO_LMCACHE_DEPLOYMENT_MODE"
	inferenceReasonLMCacheNotReady = "LMCacheNotReady"
)

// preserveLMCacheTopologyForDeployedImage removes newly generated LMCache
// sidecar fields when reconciliation intentionally keeps an older KAITO base
// image for the primary inference container. That image lacks the sidecar health
// script and may also start its in-process cache, causing a crash loop and
// duplicate host-memory use.
// It returns true when existing LMCache topology is preserved.
func preserveLMCacheTopologyForDeployedImage(existing, desired *appsv1.StatefulSet) bool {
	existingSpec := &existing.Spec.Template.Spec
	desiredSpec := &desired.Spec.Template.Spec
	if workspace.GetInferenceContainerImage(existing) == workspace.GetInferenceContainerImage(desired) ||
		hasContainer(existingSpec.InitContainers, lmCacheSidecarName) ||
		!hasContainer(desiredSpec.InitContainers, lmCacheSidecarName) {
		return false
	}

	desiredSpec.InitContainers = slices.DeleteFunc(desiredSpec.InitContainers, func(container corev1.Container) bool {
		return container.Name == lmCacheSidecarName
	})
	desiredSpec.Volumes = slices.DeleteFunc(desiredSpec.Volumes, func(volume corev1.Volume) bool {
		return volume.Name == lmCacheRuntimeVolumeName
	})
	if len(desiredSpec.Containers) > 0 {
		mainContainer := &desiredSpec.Containers[0]
		mainContainer.Env = slices.DeleteFunc(mainContainer.Env, func(env corev1.EnvVar) bool {
			return env.Name == lmCacheDeploymentModeEnvName
		})
		mainContainer.VolumeMounts = slices.DeleteFunc(mainContainer.VolumeMounts, func(mount corev1.VolumeMount) bool {
			return mount.Name == lmCacheRuntimeVolumeName
		})
		if len(existingSpec.Containers) > 0 {
			mainContainer.LivenessProbe = existingSpec.Containers[0].LivenessProbe.DeepCopy()
		}
	}
	return true
}

func hasContainer(containers []corev1.Container, name string) bool {
	return slices.ContainsFunc(containers, func(container corev1.Container) bool {
		return container.Name == name
	})
}

// detectLMCacheFailure reports sidecar status that identifies managed LMCache
// as the readiness blocker. Missing status and PodInitializing are left to
// prerequisite and scheduling checks because LMCache is not yet known to block.
func detectLMCacheFailure(pods *corev1.PodList) (reason, message string) {
	for i := range pods.Items {
		for _, status := range pods.Items[i].Status.InitContainerStatuses {
			if status.Name != lmCacheSidecarName || status.Ready {
				continue
			}
			if waiting := status.State.Waiting; waiting != nil {
				if waiting.Reason == "" || waiting.Reason == "PodInitializing" {
					continue
				}
				message := fmt.Sprintf("managed LMCache sidecar is waiting (%s)", waiting.Reason)
				if waiting.Message != "" {
					message += ": " + waiting.Message
				}
				if terminated := status.LastTerminationState.Terminated; terminated != nil {
					message += fmt.Sprintf("; last terminated with exit code %d", terminated.ExitCode)
				}
				return inferenceReasonLMCacheNotReady, message
			}
			if terminated := status.State.Terminated; terminated != nil {
				return inferenceReasonLMCacheNotReady,
					fmt.Sprintf("managed LMCache sidecar terminated (%s, exit code %d)", terminated.Reason, terminated.ExitCode)
			}
			if status.State.Running != nil {
				return inferenceReasonLMCacheNotReady, "managed LMCache sidecar is running but has not passed its readiness probe; check the lmcache-mp-server logs"
			}
		}
	}
	return "", ""
}
