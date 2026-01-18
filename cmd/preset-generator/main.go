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

package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/kaito-project/kaito/presets/workspace/generator"
)

func main() {
	token := flag.String("token", "", "Hugging Face API token")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Println("Usage: preset-generator <model_repo>")
		os.Exit(1)
	}

	modelRepo := flag.Arg(0)

	param, err := generator.GeneratePreset(modelRepo, *token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Use yaml.MapSlice to force exact ordering to match Python output
	szStr := strings.TrimSuffix(param.Metadata.ModelFileSize, "Gi")
	szVal, _ := strconv.ParseFloat(szStr, 64)

	// Construct MapSlice for VLLM params
	vllmParams := yaml.MapSlice{
		{Key: "load_format", Value: param.VLLM.ModelRunParams["load_format"]},
		{Key: "config_format", Value: param.VLLM.ModelRunParams["config_format"]},
		{Key: "tokenizer_mode", Value: param.VLLM.ModelRunParams["tokenizer_mode"]},
	}

	// Construct MapSlice for VLLM section
	vllmSection := yaml.MapSlice{
		{Key: "model_name", Value: param.VLLM.ModelName},
		{Key: "model_run_params", Value: vllmParams},
		{Key: "disallow_lora", Value: param.VLLM.DisallowLoRA},
	}

	// Construct the top-level MapSlice
	out := yaml.MapSlice{
		{Key: "attn_type", Value: param.AttnType},
		{Key: "name", Value: param.Metadata.Name},
		{Key: "architectures", Value: param.Metadata.Architectures},
		{Key: "type", Value: param.Metadata.ModelType},
		{Key: "version", Value: param.Metadata.Version},
		{Key: "download_at_runtime", Value: param.Metadata.DownloadAtRuntime},
		{Key: "download_auth_required", Value: param.Metadata.DownloadAuthRequired},
		{Key: "disk_storage_requirement", Value: param.Metadata.DiskStorageRequirement},
		{Key: "model_file_size_gb", Value: szVal},
		{Key: "bytes_per_token", Value: param.Metadata.BytesPerToken},
		{Key: "model_token_limit", Value: param.Metadata.ModelTokenLimit},
		{Key: "reasoning_parser", Value: param.Metadata.ReasoningParser},
		{Key: "tool_call_parser", Value: param.Metadata.ToolCallParser},
		{Key: "vllm", Value: vllmSection},
	}

	output, err := yaml.Marshal(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling YAML: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(output))
}
