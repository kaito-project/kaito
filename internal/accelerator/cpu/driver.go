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

// Package cpu implements the accelerator driver for CPU-only inference.
package cpu

import (
	"errors"

	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/accelerator"
	"github.com/kaito-project/kaito/pkg/catalog"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
)

// LabelExclusiveWorkload marks CPU inference pods, which run one per node.
const LabelExclusiveWorkload = "kaito.sh/cpu-inference-workload"

// Driver renders CPU inference tolerations, images and placement.
type Driver struct{}

func (Driver) Type() accelerator.Type { return accelerator.TypeCPU }

// ResourceRequirements requests no devices.
func (Driver) ResourceRequirements(int, string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{}
}

func (Driver) Tolerations() []corev1.Toleration {
	return []corev1.Toleration{{
		Effect:   corev1.TaintEffectNoSchedule,
		Value:    v1beta1.SKUCPU,
		Key:      v1beta1.LabelSKU,
		Operator: corev1.TolerationOpEqual,
	}}
}

func (Driver) PartitionTolerations(string) []corev1.Toleration { return nil }

func (Driver) SharedDeviceEnv() []corev1.EnvVar { return nil }

// InferenceBaseImage supports only vLLM; never silently select an image for
// an unsupported CPU runtime.
func (Driver) InferenceBaseImage(runtime pkgmodel.RuntimeName) (pkgmodel.Metadata, error) {
	if runtime != pkgmodel.RuntimeNameVLLM {
		return pkgmodel.Metadata{}, errors.New("CPU preset inference supports only vllm")
	}
	return catalog.MustGet("base-cpu"), nil
}

// ExclusiveNodeLabels keeps CPU inference pods on separate nodes, since no
// device request isolates them.
func (Driver) ExclusiveNodeLabels() map[string]string {
	return map[string]string{LabelExclusiveWorkload: "true"}
}
