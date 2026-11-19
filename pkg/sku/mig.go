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

package sku

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kaito-project/kaito/pkg/accelerator/nvidia"
)

// MIGGPUConfig builds a GPUConfig from a MIG profile string. Each MIG workload
// is scheduled on exactly one hardware-isolated slice.
func MIGGPUConfig(migProfile string) (*GPUConfig, error) {
	_, memGB, err := nvidia.ParseMIGProfile(migProfile)
	if err != nil {
		return nil, fmt.Errorf("invalid MIG profile: %w", err)
	}
	return &GPUConfig{
		SKU:      "unknown",
		GPUCount: 1,
		GPUMem:   resource.MustParse(fmt.Sprintf("%dGi", memGB)),
		IsMIG:    true,
	}, nil
}
