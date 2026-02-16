package presets

import "sort"

type Preset struct {
	ModelName          string
	Family             string
	HuggingFaceRepo    string
	GPUCount           int
	MinVRAMPerGPUGiB   int
	TotalModelSizeGiB  float64
	DiskStorageGiB     int
	MaxModelLen        int
	DType              string
	Quantization       string
	RequiresAuth       bool
	Tags               []string
	AWSInstanceTypes   []string
	AzureInstanceTypes []string
	VLLMExtraArgs      []string

	ContainerImage   string
	ContainerCommand []string
	ContainerArgs    []string
	ContainerPort    int
	ContainerEnv     map[string]string
}

func (p Preset) IsContainerImage() bool {
	return p.ContainerImage != ""
}

func (p Preset) ServingPort() int {
	if p.ContainerPort > 0 {
		return p.ContainerPort
	}
	return 8000
}

var registry = map[string]Preset{
	// ---- Phi family ----
	"phi-3-mini-4k-instruct": {
		ModelName:          "phi-3-mini-4k-instruct",
		Family:             "phi",
		HuggingFaceRepo:    "microsoft/Phi-3-mini-4k-instruct",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   16,
		TotalModelSizeGiB:  7.6,
		DiskStorageGiB:     50,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g4dn.xlarge", "g4dn.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC4as_T4_v3", "Standard_NC8as_T4_v3"},
	},
	"phi-3-mini-128k-instruct": {
		ModelName:          "phi-3-mini-128k-instruct",
		Family:             "phi",
		HuggingFaceRepo:    "microsoft/Phi-3-mini-128k-instruct",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   16,
		TotalModelSizeGiB:  7.6,
		DiskStorageGiB:     50,
		MaxModelLen:        16384,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g4dn.xlarge", "g4dn.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC4as_T4_v3", "Standard_NC8as_T4_v3"},
	},
	"phi-3-medium-4k-instruct": {
		ModelName:          "phi-3-medium-4k-instruct",
		Family:             "phi",
		HuggingFaceRepo:    "microsoft/Phi-3-medium-4k-instruct",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  28,
		DiskStorageGiB:     100,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.2xlarge", "g5.4xlarge"},
		AzureInstanceTypes: []string{"Standard_NC24ads_A100_v4"},
	},
	"phi-3-medium-128k-instruct": {
		ModelName:          "phi-3-medium-128k-instruct",
		Family:             "phi",
		HuggingFaceRepo:    "microsoft/Phi-3-medium-128k-instruct",
		GPUCount:           2,
		MinVRAMPerGPUGiB:   24,
		TotalModelSizeGiB:  28,
		DiskStorageGiB:     100,
		MaxModelLen:        16384,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.12xlarge"},
		AzureInstanceTypes: []string{"Standard_NC48ads_A100_v4"},
	},
	"phi-3.5-mini-instruct": {
		ModelName:          "phi-3.5-mini-instruct",
		Family:             "phi",
		HuggingFaceRepo:    "microsoft/Phi-3.5-mini-instruct",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   16,
		TotalModelSizeGiB:  7.6,
		DiskStorageGiB:     50,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g4dn.xlarge", "g4dn.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC4as_T4_v3", "Standard_NC8as_T4_v3"},
	},
	"phi-3.5-moe-instruct": {
		ModelName:          "phi-3.5-moe-instruct",
		Family:             "phi",
		HuggingFaceRepo:    "microsoft/Phi-3.5-MoE-instruct",
		GPUCount:           2,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  84,
		DiskStorageGiB:     200,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC48ads_A100_v4", "Standard_NC96ads_A100_v4"},
	},
	"phi-4": {
		ModelName:          "phi-4",
		Family:             "phi",
		HuggingFaceRepo:    "microsoft/phi-4",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  28,
		DiskStorageGiB:     100,
		MaxModelLen:        16384,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.2xlarge", "g5.4xlarge"},
		AzureInstanceTypes: []string{"Standard_NC24ads_A100_v4"},
	},
	"phi-4-mini-instruct": {
		ModelName:          "phi-4-mini-instruct",
		Family:             "phi",
		HuggingFaceRepo:    "microsoft/Phi-4-mini-instruct",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   24,
		TotalModelSizeGiB:  7.15,
		DiskStorageGiB:     70,
		MaxModelLen:        131072,
		DType:              "float16",
		AWSInstanceTypes:   []string{"g5.xlarge", "g5.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC6s_v3"},
	},

	// ---- Llama family ----
	"llama-3.1-8b-instruct": {
		ModelName:          "llama-3.1-8b-instruct",
		Family:             "llama",
		HuggingFaceRepo:    "meta-llama/Llama-3.1-8B-Instruct",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   24,
		TotalModelSizeGiB:  16,
		DiskStorageGiB:     50,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		RequiresAuth:       true,
		AWSInstanceTypes:   []string{"g5.xlarge", "g5.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC6s_v3"},
	},
	"llama-3.1-70b-instruct": {
		ModelName:          "llama-3.1-70b-instruct",
		Family:             "llama",
		HuggingFaceRepo:    "meta-llama/Llama-3.1-70B-Instruct",
		GPUCount:           4,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  140,
		DiskStorageGiB:     300,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		RequiresAuth:       true,
		AWSInstanceTypes:   []string{"p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC96ads_A100_v4", "Standard_ND96asr_v4"},
	},
	"llama-3.1-405b-instruct": {
		ModelName:          "llama-3.1-405b-instruct",
		Family:             "llama",
		HuggingFaceRepo:    "meta-llama/Llama-3.1-405B-Instruct",
		GPUCount:           8,
		MinVRAMPerGPUGiB:   80,
		TotalModelSizeGiB:  810,
		DiskStorageGiB:     1500,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		RequiresAuth:       true,
		AWSInstanceTypes:   []string{"p5.48xlarge"},
		AzureInstanceTypes: []string{"Standard_ND96asr_v4"},
		VLLMExtraArgs:      []string{"--enable-prefix-caching"},
	},
	"llama-3.3-70b-instruct": {
		ModelName:          "llama-3.3-70b-instruct",
		Family:             "llama",
		HuggingFaceRepo:    "meta-llama/Llama-3.3-70B-Instruct",
		GPUCount:           4,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  140,
		DiskStorageGiB:     300,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		RequiresAuth:       true,
		AWSInstanceTypes:   []string{"p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC96ads_A100_v4", "Standard_ND96asr_v4"},
	},

	// ---- Mistral family ----
	"mistral-7b-instruct": {
		ModelName:          "mistral-7b-instruct",
		Family:             "mistral",
		HuggingFaceRepo:    "mistralai/Mistral-7B-Instruct-v0.3",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   24,
		TotalModelSizeGiB:  14.5,
		DiskStorageGiB:     50,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.xlarge", "g5.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC6s_v3"},
	},
	"mistral-nemo-12b-instruct": {
		ModelName:          "mistral-nemo-12b-instruct",
		Family:             "mistral",
		HuggingFaceRepo:    "mistralai/Mistral-Nemo-Instruct-2407",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  24,
		DiskStorageGiB:     100,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.2xlarge", "g5.4xlarge"},
		AzureInstanceTypes: []string{"Standard_NC24ads_A100_v4"},
	},
	"mistral-large-instruct": {
		ModelName:          "mistral-large-instruct",
		Family:             "mistral",
		HuggingFaceRepo:    "mistralai/Mistral-Large-Instruct-2407",
		GPUCount:           4,
		MinVRAMPerGPUGiB:   80,
		TotalModelSizeGiB:  246,
		DiskStorageGiB:     500,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"p4d.24xlarge", "p5.48xlarge"},
		AzureInstanceTypes: []string{"Standard_NC96ads_A100_v4", "Standard_ND96asr_v4"},
	},

	// ---- Falcon family ----
	"falcon-7b-instruct": {
		ModelName:          "falcon-7b-instruct",
		Family:             "falcon",
		HuggingFaceRepo:    "tiiuae/falcon-7b-instruct",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   16,
		TotalModelSizeGiB:  14,
		DiskStorageGiB:     50,
		MaxModelLen:        2048,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g4dn.xlarge", "g4dn.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC4as_T4_v3", "Standard_NC8as_T4_v3"},
	},
	"falcon-40b-instruct": {
		ModelName:          "falcon-40b-instruct",
		Family:             "falcon",
		HuggingFaceRepo:    "tiiuae/falcon-40b-instruct",
		GPUCount:           2,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  84,
		DiskStorageGiB:     200,
		MaxModelLen:        2048,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.12xlarge", "p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC48ads_A100_v4", "Standard_NC96ads_A100_v4"},
	},

	// ---- Qwen family ----
	"qwen-2.5-0.5b-instruct": {
		ModelName:          "qwen-2.5-0.5b-instruct",
		Family:             "qwen",
		HuggingFaceRepo:    "Qwen/Qwen2.5-0.5B-Instruct",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   16,
		TotalModelSizeGiB:  1,
		DiskStorageGiB:     20,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g4dn.xlarge"},
		AzureInstanceTypes: []string{"Standard_NC4as_T4_v3"},
	},
	"qwen-2.5-7b-instruct": {
		ModelName:          "qwen-2.5-7b-instruct",
		Family:             "qwen",
		HuggingFaceRepo:    "Qwen/Qwen2.5-7B-Instruct",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   24,
		TotalModelSizeGiB:  15,
		DiskStorageGiB:     50,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.xlarge", "g5.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC6s_v3"},
	},
	"qwen-2.5-14b-instruct": {
		ModelName:          "qwen-2.5-14b-instruct",
		Family:             "qwen",
		HuggingFaceRepo:    "Qwen/Qwen2.5-14B-Instruct",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  28,
		DiskStorageGiB:     100,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.2xlarge", "g5.4xlarge"},
		AzureInstanceTypes: []string{"Standard_NC24ads_A100_v4"},
	},
	"qwen-2.5-32b-instruct": {
		ModelName:          "qwen-2.5-32b-instruct",
		Family:             "qwen",
		HuggingFaceRepo:    "Qwen/Qwen2.5-32B-Instruct",
		GPUCount:           2,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  64,
		DiskStorageGiB:     150,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.12xlarge", "p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC48ads_A100_v4", "Standard_NC96ads_A100_v4"},
	},
	"qwen-2.5-72b-instruct": {
		ModelName:          "qwen-2.5-72b-instruct",
		Family:             "qwen",
		HuggingFaceRepo:    "Qwen/Qwen2.5-72B-Instruct",
		GPUCount:           4,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  145,
		DiskStorageGiB:     300,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC96ads_A100_v4", "Standard_ND96asr_v4"},
	},
	"qwen-2.5-coder-32b-instruct": {
		ModelName:          "qwen-2.5-coder-32b-instruct",
		Family:             "qwen",
		HuggingFaceRepo:    "Qwen/Qwen2.5-Coder-32B-Instruct",
		GPUCount:           2,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  64,
		DiskStorageGiB:     150,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		Tags:               []string{"code"},
		AWSInstanceTypes:   []string{"g5.12xlarge", "p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC48ads_A100_v4", "Standard_NC96ads_A100_v4"},
	},

	// ---- DeepSeek family ----
	"deepseek-r1-distill-qwen-1.5b": {
		ModelName:          "deepseek-r1-distill-qwen-1.5b",
		Family:             "deepseek",
		HuggingFaceRepo:    "deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   16,
		TotalModelSizeGiB:  3.1,
		DiskStorageGiB:     20,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g4dn.xlarge"},
		AzureInstanceTypes: []string{"Standard_NC4as_T4_v3"},
	},
	"deepseek-r1-distill-qwen-7b": {
		ModelName:          "deepseek-r1-distill-qwen-7b",
		Family:             "deepseek",
		HuggingFaceRepo:    "deepseek-ai/DeepSeek-R1-Distill-Qwen-7B",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   24,
		TotalModelSizeGiB:  15,
		DiskStorageGiB:     50,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.xlarge", "g5.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC6s_v3"},
	},
	"deepseek-r1-distill-qwen-14b": {
		ModelName:          "deepseek-r1-distill-qwen-14b",
		Family:             "deepseek",
		HuggingFaceRepo:    "deepseek-ai/DeepSeek-R1-Distill-Qwen-14B",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  28,
		DiskStorageGiB:     100,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.2xlarge", "g5.4xlarge"},
		AzureInstanceTypes: []string{"Standard_NC24ads_A100_v4"},
	},
	"deepseek-r1-distill-qwen-32b": {
		ModelName:          "deepseek-r1-distill-qwen-32b",
		Family:             "deepseek",
		HuggingFaceRepo:    "deepseek-ai/DeepSeek-R1-Distill-Qwen-32B",
		GPUCount:           2,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  64,
		DiskStorageGiB:     150,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.12xlarge", "p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC48ads_A100_v4", "Standard_NC96ads_A100_v4"},
	},
	"deepseek-r1-distill-llama-8b": {
		ModelName:          "deepseek-r1-distill-llama-8b",
		Family:             "deepseek",
		HuggingFaceRepo:    "deepseek-ai/DeepSeek-R1-Distill-Llama-8B",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   24,
		TotalModelSizeGiB:  16,
		DiskStorageGiB:     50,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.xlarge", "g5.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC6s_v3"},
	},
	"deepseek-r1-distill-llama-70b": {
		ModelName:          "deepseek-r1-distill-llama-70b",
		Family:             "deepseek",
		HuggingFaceRepo:    "deepseek-ai/DeepSeek-R1-Distill-Llama-70B",
		GPUCount:           4,
		MinVRAMPerGPUGiB:   40,
		TotalModelSizeGiB:  140,
		DiskStorageGiB:     300,
		MaxModelLen:        4096,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC96ads_A100_v4", "Standard_ND96asr_v4"},
	},
	"deepseek-r1-0528": {
		ModelName:          "deepseek-r1-0528",
		Family:             "deepseek",
		HuggingFaceRepo:    "deepseek-ai/DeepSeek-R1-0528",
		GPUCount:           8,
		MinVRAMPerGPUGiB:   80,
		TotalModelSizeGiB:  641.3,
		DiskStorageGiB:     800,
		MaxModelLen:        163840,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"p5.48xlarge"},
		AzureInstanceTypes: []string{"Standard_NC80adis_H100_v5"},
		VLLMExtraArgs:      []string{"--reasoning-parser", "deepseek_r1"},
	},
	"deepseek-v3-0324": {
		ModelName:          "deepseek-v3-0324",
		Family:             "deepseek",
		HuggingFaceRepo:    "deepseek-ai/DeepSeek-V3-0324",
		GPUCount:           8,
		MinVRAMPerGPUGiB:   80,
		TotalModelSizeGiB:  641.3,
		DiskStorageGiB:     800,
		MaxModelLen:        163840,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"p5.48xlarge"},
		AzureInstanceTypes: []string{"Standard_NC80adis_H100_v5"},
	},

	// ---- Gemma family ----
	"gemma-2-2b-it": {
		ModelName:          "gemma-2-2b-it",
		Family:             "gemma",
		HuggingFaceRepo:    "google/gemma-2-2b-it",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   16,
		TotalModelSizeGiB:  5,
		DiskStorageGiB:     30,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		RequiresAuth:       true,
		AWSInstanceTypes:   []string{"g4dn.xlarge", "g4dn.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC4as_T4_v3", "Standard_NC8as_T4_v3"},
	},
	"gemma-2-9b-it": {
		ModelName:          "gemma-2-9b-it",
		Family:             "gemma",
		HuggingFaceRepo:    "google/gemma-2-9b-it",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   24,
		TotalModelSizeGiB:  18,
		DiskStorageGiB:     50,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		RequiresAuth:       true,
		AWSInstanceTypes:   []string{"g5.xlarge", "g5.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC6s_v3"},
	},
	"gemma-2-27b-it": {
		ModelName:          "gemma-2-27b-it",
		Family:             "gemma",
		HuggingFaceRepo:    "google/gemma-2-27b-it",
		GPUCount:           2,
		MinVRAMPerGPUGiB:   24,
		TotalModelSizeGiB:  54,
		DiskStorageGiB:     150,
		MaxModelLen:        8192,
		DType:              "bfloat16",
		RequiresAuth:       true,
		AWSInstanceTypes:   []string{"g5.12xlarge"},
		AzureInstanceTypes: []string{"Standard_NC48ads_A100_v4"},
	},
	"gemma-3-4b-instruct": {
		ModelName:          "gemma-3-4b-instruct",
		Family:             "gemma",
		HuggingFaceRepo:    "google/gemma-3-4b-it",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   16,
		TotalModelSizeGiB:  8.6,
		DiskStorageGiB:     60,
		MaxModelLen:        131072,
		DType:              "bfloat16",
		RequiresAuth:       true,
		AWSInstanceTypes:   []string{"g4dn.xlarge", "g4dn.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NV36ads_A10_v5"},
	},
	"gemma-3-27b-instruct": {
		ModelName:          "gemma-3-27b-instruct",
		Family:             "gemma",
		HuggingFaceRepo:    "google/gemma-3-27b-it",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   80,
		TotalModelSizeGiB:  54.8,
		DiskStorageGiB:     200,
		MaxModelLen:        131072,
		DType:              "bfloat16",
		RequiresAuth:       true,
		AWSInstanceTypes:   []string{"p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC24ads_A100_v4"},
	},

	// ---- Container image presets ----
	"falcon-7b-instruct-container": {
		ModelName:          "falcon-7b-instruct",
		Family:             "falcon",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   16,
		DiskStorageGiB:     50,
		DType:              "float16",
		AWSInstanceTypes:   []string{"g4dn.xlarge", "g4dn.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC4as_T4_v3", "Standard_NC8as_T4_v3"},
		ContainerImage:     "mcr.microsoft.com/aks/kaito/kaito-falcon-7b-instruct:0.2.0",
		ContainerCommand:   []string{"python3", "/workspace/vllm/inference_api.py"},
		ContainerArgs: []string{
			"--model", "/workspace/vllm/weights",
			"--dtype", "float16",
			"--tensor-parallel-size", "1",
		},
	},
	"falcon-40b-instruct-container": {
		ModelName:          "falcon-40b-instruct",
		Family:             "falcon",
		GPUCount:           2,
		MinVRAMPerGPUGiB:   40,
		DiskStorageGiB:     200,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.12xlarge", "p4d.24xlarge"},
		AzureInstanceTypes: []string{"Standard_NC48ads_A100_v4", "Standard_NC96ads_A100_v4"},
		ContainerImage:     "mcr.microsoft.com/aks/kaito/kaito-falcon-40b-instruct:0.2.0",
		ContainerCommand:   []string{"python3", "/workspace/vllm/inference_api.py"},
		ContainerArgs: []string{
			"--model", "/workspace/vllm/weights",
			"--dtype", "bfloat16",
			"--tensor-parallel-size", "2",
		},
	},
	"llama-3.1-8b-instruct-container": {
		ModelName:          "llama-3.1-8b-instruct",
		Family:             "llama",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   24,
		DiskStorageGiB:     50,
		DType:              "bfloat16",
		RequiresAuth:       true,
		AWSInstanceTypes:   []string{"g5.xlarge", "g5.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC6s_v3"},
		ContainerImage:     "mcr.microsoft.com/aks/kaito/kaito-llama-3.1-8b-instruct:0.1.0",
		ContainerCommand:   []string{"python3", "/workspace/vllm/inference_api.py"},
		ContainerArgs: []string{
			"--model", "/workspace/vllm/weights",
			"--dtype", "bfloat16",
			"--tensor-parallel-size", "1",
			"--max-model-len", "8192",
		},
	},
	"mistral-7b-instruct-container": {
		ModelName:          "mistral-7b-instruct",
		Family:             "mistral",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   24,
		DiskStorageGiB:     50,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.xlarge", "g5.2xlarge"},
		AzureInstanceTypes: []string{"Standard_NC6s_v3"},
		ContainerImage:     "mcr.microsoft.com/aks/kaito/kaito-mistral-7b-instruct:0.1.0",
		ContainerCommand:   []string{"python3", "/workspace/vllm/inference_api.py"},
		ContainerArgs: []string{
			"--model", "/workspace/vllm/weights",
			"--dtype", "bfloat16",
			"--tensor-parallel-size", "1",
			"--max-model-len", "8192",
		},
	},
	"phi-4-container": {
		ModelName:          "phi-4",
		Family:             "phi",
		GPUCount:           1,
		MinVRAMPerGPUGiB:   40,
		DiskStorageGiB:     100,
		DType:              "bfloat16",
		AWSInstanceTypes:   []string{"g5.2xlarge", "g5.4xlarge"},
		AzureInstanceTypes: []string{"Standard_NC24ads_A100_v4"},
		ContainerImage:     "mcr.microsoft.com/aks/kaito/kaito-phi-4:0.1.0",
		ContainerCommand:   []string{"python3", "/workspace/vllm/inference_api.py"},
		ContainerArgs: []string{
			"--model", "/workspace/vllm/weights",
			"--dtype", "bfloat16",
			"--tensor-parallel-size", "1",
			"--max-model-len", "16384",
		},
	},
}

func Get(modelName string) (Preset, bool) {
	p, ok := registry[modelName]
	return p, ok
}

func List() []Preset {
	result := make([]Preset, 0, len(registry))
	for _, p := range registry {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ModelName < result[j].ModelName
	})
	return result
}

func ListByFamily(family string) []Preset {
	var result []Preset
	for _, p := range registry {
		if p.Family == family {
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ModelName < result[j].ModelName
	})
	return result
}

func ListModelNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
