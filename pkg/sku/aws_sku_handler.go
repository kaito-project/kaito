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
	supportedSKUs := []GPUConfig{
		{SKU: "p2.xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("12Gi"), GPUModel: "NVIDIA K80"},
		{SKU: "p2.8xlarge", GPUCount: 8, GPUMemGiB: resource.MustParse("96Gi"), GPUModel: "NVIDIA K80"},
		{SKU: "p2.16xlarge", GPUCount: 16, GPUMemGiB: resource.MustParse("192Gi"), GPUModel: "NVIDIA K80"},
		{SKU: "p3.2xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA V100"},
		{SKU: "p3.8xlarge", GPUCount: 4, GPUMemGiB: resource.MustParse("64Gi"), GPUModel: "NVIDIA V100"},
		{SKU: "p3.16xlarge", GPUCount: 8, GPUMemGiB: resource.MustParse("128Gi"), GPUModel: "NVIDIA V100"},
		{SKU: "p3dn.24xlarge", GPUCount: 8, GPUMemGiB: resource.MustParse("256Gi"), GPUModel: "NVIDIA V100"},
		{SKU: "p4d.24xlarge", GPUCount: 8, GPUMemGiB: resource.MustParse("320Gi"), GPUModel: "NVIDIA A100", NVMeDiskEnabled: true},
		{SKU: "p4de.24xlarge", GPUCount: 8, GPUMemGiB: resource.MustParse("640Gi"), GPUModel: "NVIDIA A100", NVMeDiskEnabled: true},
		{SKU: "p5.48xlarge", GPUCount: 8, GPUMemGiB: resource.MustParse("640Gi"), GPUModel: "NVIDIA H100", NVMeDiskEnabled: true},
		{SKU: "p5e.48xlarge", GPUCount: 8, GPUMemGiB: resource.MustParse("1128Gi"), GPUModel: "NVIDIA H200", NVMeDiskEnabled: true},
		{SKU: "p5en.48xlarge", GPUCount: 8, GPUMemGiB: resource.MustParse("1128Gi"), GPUModel: "NVIDIA H200", NVMeDiskEnabled: true},
		{SKU: "g6.xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.2xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.4xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.8xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.16xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "gr6.4xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "gr6.8xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.12xlarge", GPUCount: 4, GPUMemGiB: resource.MustParse("96Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.24xlarge", GPUCount: 4, GPUMemGiB: resource.MustParse("96Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.48xlarge", GPUCount: 8, GPUMemGiB: resource.MustParse("192Gi"), GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g5g.xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA T4"},
		{SKU: "g5g.2xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA T4"},
		{SKU: "g5g.4xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA T4"},
		{SKU: "g5g.8xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA T4"},
		{SKU: "g5g.16xlarge", GPUCount: 2, GPUMemGiB: resource.MustParse("32Gi"), GPUModel: "NVIDIA T4"},
		{SKU: "g5g.metal", GPUCount: 2, GPUMemGiB: resource.MustParse("32Gi"), GPUModel: "NVIDIA T4"},
		{SKU: "g5.xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.2xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.4xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.8xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.12xlarge", GPUCount: 4, GPUMemGiB: resource.MustParse("96Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.16xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("24Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.24xlarge", GPUCount: 4, GPUMemGiB: resource.MustParse("96Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.48xlarge", GPUCount: 8, GPUMemGiB: resource.MustParse("192Gi"), GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g4dn.xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.2xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.4xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.8xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.16xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.12xlarge", GPUCount: 4, GPUMemGiB: resource.MustParse("64Gi"), GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.metal", GPUCount: 8, GPUMemGiB: resource.MustParse("128Gi"), GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4ad.xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("8Gi"), GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.2xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("8Gi"), GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.4xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("8Gi"), GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.8xlarge", GPUCount: 2, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.16xlarge", GPUCount: 4, GPUMemGiB: resource.MustParse("32Gi"), GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g3s.xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("8Gi"), GPUModel: "NVIDIA M60"},
		{SKU: "g3s.4xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("8Gi"), GPUModel: "NVIDIA M60"},
		{SKU: "g3s.8xlarge", GPUCount: 2, GPUMemGiB: resource.MustParse("16Gi"), GPUModel: "NVIDIA M60"},
		{SKU: "g3s.16xlarge", GPUCount: 4, GPUMemGiB: resource.MustParse("32Gi"), GPUModel: "NVIDIA M60"},
		//accelerator optimized
		{SKU: "trn1.2xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("32Gi"), GPUModel: "AWS Trainium accelerators", NVMeDiskEnabled: true},
		{SKU: "trn1.32xlarge", GPUCount: 16, GPUMemGiB: resource.MustParse("512Gi"), GPUModel: "AWS Trainium accelerators", NVMeDiskEnabled: true},
		{SKU: "trn1n.32xlarge", GPUCount: 16, GPUMemGiB: resource.MustParse("512Gi"), GPUModel: "AWS Trainium accelerators", NVMeDiskEnabled: true},
		{SKU: "inf2.xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("32Gi"), GPUModel: "AWS Inferentia2 accelerators"},
		{SKU: "inf2.8xlarge", GPUCount: 1, GPUMemGiB: resource.MustParse("32Gi"), GPUModel: "AWS Inferentia2 accelerators"},
		{SKU: "inf2.24xlarge", GPUCount: 6, GPUMemGiB: resource.MustParse("192Gi"), GPUModel: "AWS Inferentia2 accelerators"},
		{SKU: "inf2.48xlarge", GPUCount: 12, GPUMemGiB: resource.MustParse("384Gi"), GPUModel: "AWS Inferentia2 accelerators"},
	}
	return NewGeneralSKUHandler(supportedSKUs)
}
