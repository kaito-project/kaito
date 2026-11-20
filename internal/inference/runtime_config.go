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

package inference

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/kaito-project/kaito/pkg/catalog/hfgen"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
)

// inferenceRuntimeConfig contains the inference settings needed during workload generation.
type inferenceRuntimeConfig struct {
	VLLM                        map[string]interface{} `yaml:"vllm"`
	KVCacheCPUMemoryUtilization *float64               `yaml:"kv_cache_cpu_memory_utilization"`
}

// parseInferenceRuntimeConfig parses inference_config.yaml settings used by the controller.
func parseInferenceRuntimeConfig(content string) (inferenceRuntimeConfig, error) {
	config := inferenceRuntimeConfig{}
	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return inferenceRuntimeConfig{}, err
	}
	return config, nil
}

func adaptiveVerificationSupported(params *pkgmodel.PresetParam, userConfig map[string]any, pipelineParallel bool, gpuConfig *sku.GPUConfig) (bool, string, error) {
	runParams := params.VLLM.ModelRunParams
	pipelineSize := "1"
	if pipelineParallel {
		pipelineSize = "2"
	} else if value, ok := runParams["pipeline-parallel-size"]; ok {
		pipelineSize = value
	}
	if value, ok := userConfig["pipeline-parallel-size"]; ok {
		pipelineSize = fmt.Sprint(value)
	}
	size, err := strconv.Atoi(pipelineSize)
	if err != nil || size < 1 {
		return false, "", fmt.Errorf("pipeline-parallel-size must be a positive integer, got %q", pipelineSize)
	}

	eagerValue := "false"
	if value, ok := runParams["enforce-eager"]; ok {
		eagerValue = value
		if value == "" {
			eagerValue = "true"
		}
	}
	if value, ok := userConfig["enforce-eager"]; ok {
		eagerValue = fmt.Sprint(value)
	}
	eager, err := strconv.ParseBool(eagerValue)
	if err != nil {
		return false, "", fmt.Errorf("invalid enforce-eager setting %q: %w", eagerValue, err)
	}

	mode := ""
	if gpuConfig != nil {
		mode, _ = hfgen.ResolveCUDAGraphMode(params.VLLM.ModelName, gpuConfig.GPUModel)
	}
	if value, ok := runParams["compilation-config.cudagraph_mode"]; ok {
		mode = value
	}
	var modeOverride any
	overridden := false
	if raw, ok := userConfig["compilation-config"]; ok {
		value, ok := raw.(string)
		if !ok {
			return false, "", fmt.Errorf("compilation-config must be a JSON string")
		}
		var compilation map[string]any
		if err := json.Unmarshal([]byte(value), &compilation); err != nil {
			return false, "", fmt.Errorf("parse compilation-config: %w", err)
		}
		if compilation == nil {
			return false, "", fmt.Errorf("compilation-config must be a JSON object")
		}
		modeOverride, overridden = compilation["cudagraph_mode"]
	}
	// vLLM applies dotted configuration arguments after whole-object JSON.
	if value, ok := userConfig["compilation-config.cudagraph_mode"]; ok {
		modeOverride, overridden = value, true
	}
	if overridden {
		mode = fmt.Sprint(modeOverride)
		if mode == "" {
			return false, "", fmt.Errorf("compilation-config.cudagraph_mode must not be empty")
		}
	}
	mode = strings.ToUpper(mode)
	switch mode {
	case "0":
		mode = "NONE"
	case "1":
		mode = "PIECEWISE"
	case "2":
		mode = "FULL"
	}
	compatible := size == 1 && !eager
	switch mode {
	case "", "FULL", "FULL_DECODE_ONLY", "FULL_AND_PIECEWISE":
	case "NONE", "PIECEWISE":
		compatible = false
	default:
		return false, "", fmt.Errorf("unsupported compilation-config.cudagraph_mode %q", mode)
	}
	if overridden {
		return compatible, mode, nil
	}
	return compatible, "", nil
}
