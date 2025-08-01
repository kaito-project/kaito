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

func NewAwsSKUHandler() CloudSKUHandler {
	// Reference: https://aws.amazon.com/ec2/instance-types/
	supportedSKUs := []GPUConfig{
		{SKU: "p2.xlarge", GPUCount: 1, GPUMemGB: 12, GPUModel: "NVIDIA K80"},
		{SKU: "p2.8xlarge", GPUCount: 8, GPUMemGB: 96, GPUModel: "NVIDIA K80"},
		{SKU: "p2.16xlarge", GPUCount: 16, GPUMemGB: 192, GPUModel: "NVIDIA K80"},
		{SKU: "p3.2xlarge", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA V100"},
		{SKU: "p3.8xlarge", GPUCount: 4, GPUMemGB: 64, GPUModel: "NVIDIA V100"},
		{SKU: "p3.16xlarge", GPUCount: 8, GPUMemGB: 128, GPUModel: "NVIDIA V100"},
		{SKU: "p3dn.24xlarge", GPUCount: 8, GPUMemGB: 256, GPUModel: "NVIDIA V100"},
		{SKU: "p4d.24xlarge", GPUCount: 8, GPUMemGB: 320, GPUModel: "NVIDIA A100", NVMeDiskEnabled: true},
		{SKU: "p4de.24xlarge", GPUCount: 8, GPUMemGB: 640, GPUModel: "NVIDIA A100", NVMeDiskEnabled: true},
		{SKU: "p5.48xlarge", GPUCount: 8, GPUMemGB: 640, GPUModel: "NVIDIA H100", NVMeDiskEnabled: true},
		{SKU: "p5e.48xlarge", GPUCount: 8, GPUMemGB: 1128, GPUModel: "NVIDIA H200", NVMeDiskEnabled: true},
		{SKU: "p5en.48xlarge", GPUCount: 8, GPUMemGB: 1128, GPUModel: "NVIDIA H200", NVMeDiskEnabled: true},
		{SKU: "g6.xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.2xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.4xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.8xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.16xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "gr6.4xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "gr6.8xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.12xlarge", GPUCount: 4, GPUMemGB: 96, GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.24xlarge", GPUCount: 4, GPUMemGB: 96, GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g6.48xlarge", GPUCount: 8, GPUMemGB: 192, GPUModel: "NVIDIA L4", NVMeDiskEnabled: true},
		{SKU: "g5g.xlarge", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4"},
		{SKU: "g5g.2xlarge", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4"},
		{SKU: "g5g.4xlarge", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4"},
		{SKU: "g5g.8xlarge", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4"},
		{SKU: "g5g.16xlarge", GPUCount: 2, GPUMemGB: 32, GPUModel: "NVIDIA T4"},
		{SKU: "g5g.metal", GPUCount: 2, GPUMemGB: 32, GPUModel: "NVIDIA T4"},
		{SKU: "g5.xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.2xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.4xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.8xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.12xlarge", GPUCount: 4, GPUMemGB: 96, GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.16xlarge", GPUCount: 1, GPUMemGB: 24, GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.24xlarge", GPUCount: 4, GPUMemGB: 96, GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g5.48xlarge", GPUCount: 8, GPUMemGB: 192, GPUModel: "NVIDIA A10G", NVMeDiskEnabled: true},
		{SKU: "g4dn.xlarge", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.2xlarge", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.4xlarge", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.8xlarge", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.16xlarge", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.12xlarge", GPUCount: 4, GPUMemGB: 64, GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4dn.metal", GPUCount: 8, GPUMemGB: 128, GPUModel: "NVIDIA T4", NVMeDiskEnabled: true},
		{SKU: "g4ad.xlarge", GPUCount: 1, GPUMemGB: 8, GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.2xlarge", GPUCount: 1, GPUMemGB: 8, GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.4xlarge", GPUCount: 1, GPUMemGB: 8, GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.8xlarge", GPUCount: 2, GPUMemGB: 16, GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g4ad.16xlarge", GPUCount: 4, GPUMemGB: 32, GPUModel: "AMD Radeon Pro V520", NVMeDiskEnabled: true},
		{SKU: "g3s.xlarge", GPUCount: 1, GPUMemGB: 8, GPUModel: "NVIDIA M60"},
		{SKU: "g3s.4xlarge", GPUCount: 1, GPUMemGB: 8, GPUModel: "NVIDIA M60"},
		{SKU: "g3s.8xlarge", GPUCount: 2, GPUMemGB: 16, GPUModel: "NVIDIA M60"},
		{SKU: "g3s.16xlarge", GPUCount: 4, GPUMemGB: 32, GPUModel: "NVIDIA M60"},
		//accelerator optimized
		{SKU: "trn1.2xlarge", GPUCount: 1, GPUMemGB: 32, GPUModel: "AWS Trainium accelerators", NVMeDiskEnabled: true},
		{SKU: "trn1.32xlarge", GPUCount: 16, GPUMemGB: 512, GPUModel: "AWS Trainium accelerators", NVMeDiskEnabled: true},
		{SKU: "trn1n.32xlarge", GPUCount: 16, GPUMemGB: 512, GPUModel: "AWS Trainium accelerators", NVMeDiskEnabled: true},
		{SKU: "inf2.xlarge", GPUCount: 1, GPUMemGB: 32, GPUModel: "AWS Inferentia2 accelerators"},
		{SKU: "inf2.8xlarge", GPUCount: 1, GPUMemGB: 32, GPUModel: "AWS Inferentia2 accelerators"},
		{SKU: "inf2.24xlarge", GPUCount: 6, GPUMemGB: 192, GPUModel: "AWS Inferentia2 accelerators"},
		{SKU: "inf2.48xlarge", GPUCount: 12, GPUMemGB: 384, GPUModel: "AWS Inferentia2 accelerators"},
	}
	return NewGeneralSKUHandler(supportedSKUs)
}
