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

func NewArcSKUHandler() CloudSKUHandler {
	// The SKU including Azure Local official sku: https://learn.microsoft.com/en-us/azure/aks/aksarc/scale-requirements
	supportedSKUs := []GPUConfig{
		{SKU: "Standard_NK6", GPUCount: 1, GPUMemGB: 8, GPUModel: "NVIDIA T4"},
		{SKU: "Standard_NK12", GPUCount: 2, GPUMemGB: 16, GPUModel: "NVIDIA T4"},
		{SKU: "Standard_NC4_A2", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA A2"},
		{SKU: "Standard_NC8_A2", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA A2"},
		{SKU: "Standard_NC16_A2", GPUCount: 2, GPUMemGB: 32, GPUModel: "NVIDIA A2"},
		{SKU: "Standard_NC32_A2", GPUCount: 2, GPUMemGB: 32, GPUModel: "NVIDIA A2"},
		{SKU: "Standard_NC4_A16", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA A16"},
		{SKU: "Standard_NC8_A16", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA A16"},
		{SKU: "Standard_NC16_A16", GPUCount: 2, GPUMemGB: 32, GPUModel: "NVIDIA A16"},
		{SKU: "Standard_NC32_A16", GPUCount: 2, GPUMemGB: 32, GPUModel: "NVIDIA A16"},
	}
	return NewGeneralSKUHandler(supportedSKUs)
}
