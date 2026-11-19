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

// Package accelerator defines vendor-neutral accelerator types. Vendor
// specifics live in subpackages such as accelerator/nvidia.
package accelerator

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Type identifies an accelerator vendor family.
type Type string

const (
	TypeNVIDIA Type = "nvidia"
	TypeAMD    Type = "amd"
	// TypeNeuron is AWS Inferentia and Trainium.
	TypeNeuron Type = "neuron"
	TypeCPU    Type = "cpu"
)

// Device describes the accelerators on a node.
type Device struct {
	Model string
	Count int
	// Memory is the total memory of all Count devices.
	Memory            resource.Quantity
	ComputeCapability float64
	// Partitioned reports that each device is a slice of a physical device,
	// such as an NVIDIA MIG instance.
	Partitioned bool
}

// Vendor reads accelerator devices from the node labels a vendor publishes.
type Vendor interface {
	Type() Type
	DeviceFromNode(node *corev1.Node) (*Device, error)
	// CanonicalModel maps a product name, such as "NVIDIA-A100-SXM4-80GB",
	// to the model name used in compatibility rules, or returns it unchanged.
	CanonicalModel(model string) string
}
