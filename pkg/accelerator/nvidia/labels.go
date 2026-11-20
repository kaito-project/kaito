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

package nvidia

// Node labels set by NVIDIA GPU Feature Discovery.
const (
	LabelGPUProduct       = "nvidia.com/gpu.product"
	LabelGPUCount         = "nvidia.com/gpu.count"
	LabelGPUMemory        = "nvidia.com/gpu.memory"
	LabelCUDAComputeMajor = "nvidia.com/cuda.compute.major"
	LabelCUDAComputeMinor = "nvidia.com/cuda.compute.minor"
)

// MIG-related node labels set by the NVIDIA GPU Operator's mig-manager.
// LabelMIGConfig holds the requested/applied MIG partition layout (e.g.
// "all-2g.24gb" or "all-disabled"); LabelMIGConfigState is "success" once
// the layout has been applied.
const (
	LabelMIGConfig      = "nvidia.com/mig.config"
	LabelMIGConfigState = "nvidia.com/mig.config.state"

	// MIGConfigDisabled is the mig.config value that means MIG is turned off.
	MIGConfigDisabled = "all-disabled"
	// MIGConfigStateSuccess is the mig.config.state value that means the
	// requested MIG layout has been applied successfully.
	MIGConfigStateSuccess = "success"
)
