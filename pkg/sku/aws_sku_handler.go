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

import "k8s.io/apimachinery/pkg/api/resource"

func NewAwsSKUHandler() CloudSKUHandler {
	// Reference: https://aws.amazon.com/ec2/instance-types/
	// KAITO only supports GPUs with CUDA compute capability >= 8.0 (Ampere and newer).
	// Older architectures (K80, V100, T4, M60, etc.) have been removed.
	supportedSKUs := []GPUConfig{
		{SKU: "p4d.24xlarge", GPUCount: 8, GPUMem: resource.MustParse("320Gi"), GPUModel: "NVIDIA A100", NVMeDiskEnabled: true, CUDAComputeCapability: 8.0},
		{SKU: "p4de.24xlarge", GPUCount: 8, GPUMem: resource.MustParse("640Gi"), GPUModel: "NVIDIA A100", NVMeDiskEnabled: true, CUDAComputeCapability: 8.0},
		{SKU: "p5.48xlarge", GPUCount: 8, GPUMem: resource.MustParse("640Gi"), GPUModel: "NVIDIA H100", NVMeDiskEnabled: true, CUDAComputeCapability: 9.0},
		{SKU: "p5e.48xlarge", GPUCount: 8, GPUMem: resource.MustParse("1128Gi"), GPUModel: "NVIDIA H200", NVMeDiskEnabled: true, CUDAComputeCapability: 9.0},
		{SKU: "p5en.48xlarge", GPUCount: 8, GPUMem: resource.MustParse("1128Gi"), GPUModel: "NVIDIA H200", NVMeDiskEnabled: true, CUDAComputeCapability: 9.0},
		{SKU: "g6.xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true, CUDAComputeCapability: 8.9},
		{SKU: "g6.2xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true, CUDAComputeCapability: 8.9},
		{SKU: "g6.4xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true, CUDAComputeCapability: 8.9},
		{SKU: "g6.8xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true, CUDAComputeCapability: 8.9},
		{SKU: "g6.16xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true, CUDAComputeCapability: 8.9},
		{SKU: "gr6.4xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true, CUDAComputeCapability: 8.9},
		{SKU: "gr6.8xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true, CUDAComputeCapability: 8.9},
		{SKU: "g6.12xlarge", GPUCount: 4, GPUMem: resource.MustParse("96Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true, CUDAComputeCapability: 8.9},
		{SKU: "g6.24xlarge", GPUCount: 4, GPUMem: resource.MustParse("96Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true, CUDAComputeCapability: 8.9},
		{SKU: "g6.48xlarge", GPUCount: 8, GPUMem: resource.MustParse("192Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true, CUDAComputeCapability: 8.9},
		{SKU: "g5.xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true, CUDAComputeCapability: 8.6},
		{SKU: "g5.2xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true, CUDAComputeCapability: 8.6},
		{SKU: "g5.4xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true, CUDAComputeCapability: 8.6},
		{SKU: "g5.8xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true, CUDAComputeCapability: 8.6},
		{SKU: "g5.12xlarge", GPUCount: 4, GPUMem: resource.MustParse("96Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true, CUDAComputeCapability: 8.6},
		{SKU: "g5.16xlarge", GPUCount: 1, GPUMem: resource.MustParse("24Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true, CUDAComputeCapability: 8.6},
		{SKU: "g5.24xlarge", GPUCount: 4, GPUMem: resource.MustParse("96Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true, CUDAComputeCapability: 8.6},
		{SKU: "g5.48xlarge", GPUCount: 8, GPUMem: resource.MustParse("192Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true, CUDAComputeCapability: 8.6},
		{SKU: "g4ad.xlarge", GPUCount: 1, GPUMem: resource.MustParse("8Gi"), GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.2xlarge", GPUCount: 1, GPUMem: resource.MustParse("8Gi"), GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.4xlarge", GPUCount: 1, GPUMem: resource.MustParse("8Gi"), GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.8xlarge", GPUCount: 2, GPUMem: resource.MustParse("16Gi"), GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.16xlarge", GPUCount: 4, GPUMem: resource.MustParse("32Gi"), GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		//accelerator optimized
		{SKU: "trn1.2xlarge", GPUCount: 1, GPUMem: resource.MustParse("32Gi"), GPUModel: "AWS Trainium accelerators", NVMeDiskEnabled: true},
		{SKU: "trn1.32xlarge", GPUCount: 16, GPUMem: resource.MustParse("512Gi"), GPUModel: "AWS Trainium accelerators", NVMeDiskEnabled: true},
		{SKU: "trn1n.32xlarge", GPUCount: 16, GPUMem: resource.MustParse("512Gi"), GPUModel: "AWS Trainium accelerators", NVMeDiskEnabled: true},
		{SKU: "inf2.xlarge", GPUCount: 1, GPUMem: resource.MustParse("32Gi"), GPUModel: "AWS Inferentia2 accelerators"},
		{SKU: "inf2.8xlarge", GPUCount: 1, GPUMem: resource.MustParse("32Gi"), GPUModel: "AWS Inferentia2 accelerators"},
		{SKU: "inf2.24xlarge", GPUCount: 6, GPUMem: resource.MustParse("192Gi"), GPUModel: "AWS Inferentia2 accelerators"},
		{SKU: "inf2.48xlarge", GPUCount: 12, GPUMem: resource.MustParse("384Gi"), GPUModel: "AWS Inferentia2 accelerators"},
	}
	return NewGeneralSKUHandler(supportedSKUs)
}
