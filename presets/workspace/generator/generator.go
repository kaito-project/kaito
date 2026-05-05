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

package generator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/kaito-project/kaito/pkg/model"
)

const (
	SystemFileDiskSizeGiB  = 80
	DefaultModelTokenLimit = 2048
	HuggingFaceWebsite     = "https://huggingface.co"
)

// Please update the following model-specific configurations when adding new models to model catalog
var (
	safetensorRegex = regexp.MustCompile(`.*\.safetensors`)
	binRegex        = regexp.MustCompile(`.*\.bin`)
	mistralRegex    = regexp.MustCompile(`consolidated.*\.safetensors`)
	// source: https://github.com/vllm-project/vllm/blob/main/vllm/reasoning/__init__.py
	reasoningParserModeNamePrefixMap = map[string]string{
		"deepseek-r1":  "deepseek_r1",
		"deepseek-v3":  "deepseek_v3",
		"ernie-4.5":    "ernie45",
		"gemma-4":      "gemma4",
		"glm-4.5":      "glm45",
		"holo2":        "holo2",
		"hunyuan-a13b": "hunyuan_a13b",
		"granite-3.2":  "granite",
		"kimi-k2":      "kimi_k2",
		"minimax-m2":   "minimax_m2_append_think",
		"olmo-3":       "olmo3",
		"qwen3":        "qwen3",
		"qwq-32b":      "deepseek_r1",
		"step3":        "step3",
	}
	reasoningParserArchMap = map[string]string{
		"DeepseekV3ForCausalLM":                  "deepseek_v3",
		"Ernie4_5_VLMoeForConditionalGeneration": "ernie45",
		"Ernie4_5_MoeForCausalLM":                "ernie45",
		"Gemma4ForConditionalGeneration":         "gemma4",
		"Glm4MoeForCausalLM":                     "glm45",
		"HunYuanMoEV1ForCausalLM":                "hunyuan_a13b",
		"GraniteForCausalLM":                     "granite",
		"KimiK2ForCausalLM":                      "kimi_k2",
		"MiniMaxM2ForCausalLM":                   "minimax_m2_append_think",
		"MistralForCausalLM":                     "mistral",
		"NemotronForCausalLM":                    "nemotron_v3",
		"NemotronHForCausalLM":                   "nemotron_v3",
		"NemotronH_Nano_VL_V2":                   "nemotron_v3",
		"OlmoForCausalLM":                        "olmo3",
		"Qwen3ForCausalLM":                       "qwen3",
		"Qwen3MoeForCausalLM":                    "qwen3",
		"GptOssForCausalLM":                      "openai_gptoss",
		"Step3TextForCausalLM":                   "step3",
		"Step3VLForConditionalGeneration":        "step3",
	}

	// source: https://github.com/vllm-project/vllm/blob/main/vllm/tool_parsers/__init__.py
	// key is model name prefix, value is ToolCallParser mode name
	toolCallParserModeNamePrefixMap = map[string]string{
		"hermes-2":      "hermes",
		"hermes-3":      "hermes",
		"mistral":       "mistral",
		"meta-llama-3":  "llama3_json",
		"meta-llama-4":  "llama4_pythonic",
		"granite-3":     "granite",
		"granite-4":     "hermes",
		"internlm":      "internlm",
		"ai21-jamba":    "jamba",
		"llama-xlama":   "xlam",
		"xlam":          "xlam",
		"qwq-32b":       "hermes",
		"qwen2.5":       "hermes",
		"minimax":       "minimax",
		"deepseek-r1":   "deepseek_v3",
		"deepseek-v3":   "deepseek_v3",
		"deepseek-v3.1": "deepseek_v31",
		"deepseek-v3.2": "deepseek_v32",
		"kimi_k2":       "kimi_k2",
		"hunyuan-a13b":  "hunyuan_a13b",
		"longcat":       "longcat",
		"glm-4":         "glm45",
		"glm-4.7":       "glm47",
		"qwen3":         "hermes",
		"qwen3-coder":   "qwen3_xml",
		"olmo-3":        "olmo3",
		"gigachat3":     "gigachat3",
		"ernie-4.5":     "ernie45",
		"phi4-mini":     "phi4_mini_json",
		"step3p5":       "step3p5",
		"step3":         "step3",
		"seed-oss":      "seed_oss",
		"gemma-3":       "functiongemma",
		"gemma-4":       "gemma4",
	}

	// key is model architecture name, value is ToolCallParser mode name
	toolCallParserArchMap = map[string]string{
		"MistralForCausalLM":                     "mistral",
		"MistralLarge3ForCausalLM":               "mistral",
		"LlamaForCausalLM":                       "llama3_json",
		"Llama4ForConditionalGeneration":         "llama4_pythonic",
		"GraniteForCausalLM":                     "granite",
		"GraniteMoeForCausalLM":                  "granite",
		"GraniteMoeHybridForCausalLM":            "hermes",
		"GPTBigCodeForCausalLM":                  "granite-20b-fc",
		"InternLM2ForCausalLM":                   "internlm",
		"JambaForCausalLM":                       "jamba",
		"Qwen2ForCausalLM":                       "hermes",
		"Qwen3ForCausalLM":                       "hermes",
		"Qwen3MoeForCausalLM":                    "qwen3_xml",
		"MiniMaxM1ForCausalLM":                   "minimax",
		"MiniMaxM2ForCausalLM":                   "minimax_m2",
		"DeepseekV3ForCausalLM":                  "deepseek_v3",
		"DeepseekV32ForCausalLM":                 "deepseek_v32",
		"GptOssForCausalLM":                      "openai",
		"HunYuanMoEV1ForCausalLM":                "hunyuan_a13b",
		"LongcatFlashForCausalLM":                "longcat",
		"Glm4MoeForCausalLM":                     "glm45",
		"Glm47MoeForCausalLM":                    "glm47",
		"Gemma3ForCausalLM":                      "functiongemma",
		"Gemma4ForConditionalGeneration":         "gemma4",
		"Olmo3ForCausalLM":                       "olmo3",
		"SeedOssForCausalLM":                     "seed_oss",
		"Ernie4_5_VLMoeForConditionalGeneration": "ernie45",
		"Ernie4_5_MoeForCausalLM":                "ernie45",
		"Step3TextForCausalLM":                   "step3",
		"Step3p5TextForCausalLM":                 "step3p5",
		"NemotronHForCausalLM":                   "qwen3_coder",
		"NemotronH_Nano_VL_V2":                   "qwen3_coder",
		"Phi4MiniForCausalLM":                    "phi4_mini_json",
		"KimiK2ForCausalLM":                      "kimi_k2",
		"GigaChat3ForCausalLM":                   "gigachat3",
	}

	// chatTemplatePrefixMap maps model name prefixes to vllm-customized chat templates.
	// Templates are located in /workspace/chat_templates/ in the KAITO container image.
	// source: https://github.com/vllm-project/vllm/tree/main/examples
	chatTemplatePrefixMap = map[string]string{
		"deepseek-r1": "tool-chat-deepseekr1.jinja",
		"deepseek-v3": "tool-chat-deepseekv3.jinja",
		"llama-3":     "tool-chat-llama3.1-json.jinja",
		"phi-4-mini":  "tool-chat-phi4-mini.jinja",
		"qwen2.5":     "tool-chat-hermes.jinja",
	}

	// attentionBackendPrefixMap maps model name prefixes to their vLLM attention backend.
	attentionBackendPrefixMap = map[string]string{
		// flashinfer attention backend is chosen by default for LLaMA 3 models, which requires the FlashInfer library to be installed lively.
		// Pin to triton backend as a workaround.
		"llama-3": "TRITON_ATTN",
	}

	// catalogOverrides provides hardcoded values for models whose HuggingFace
	// config.json omits fields that are required in model_catalog.yaml.
	// Keys are lowercased HuggingFace repo names.
	catalogOverrides = map[string]CatalogEntry{
		// source: https://github.com/huggingface/transformers/blob/main/src/transformers/models/gemma3/configuration_gemma3.py
		"google/gemma-3-4b-it": {
			ModelTokenLimit:   131072,
			NumAttentionHeads: 8,
			NumKeyValueHeads:  4,
			HeadDim:           256,
		},
		// Based on Gemma 3 model card, the 128K context window (131072 tokens) applies to all Gemma 3 4B/12B/27B sizes
		// source: https://huggingface.co/google/gemma-3-27b-it
		"google/gemma-3-27b-it": {
			ModelTokenLimit: 131072,
		},
		"mistralai/mistral-large-3-675b-instruct-2512": {
			// source: https://docs.vllm.ai/en/v0.17.1/api/vllm/model_executor/models/mistral_large_3/
			Architectures: []string{"MistralLarge3ForCausalLM"},
			PipelineTag:   "text-generation",
		},
	}
)

type Generator struct {
	ModelRepo      string
	Token          string
	Param          model.PresetParam
	CatalogData    []byte // Optional embedded catalog YAML
	IsMistralModel bool

	// Analyzed params
	LoadFormat    string
	ConfigFormat  string
	TokenizerMode string
	ModelConfig   map[string]interface{}
}

func NewGenerator(modelRepo, token string) *Generator {
	nameParts := strings.Split(modelRepo, "/")
	modelNameSafe := strings.ToLower(nameParts[len(nameParts)-1])

	gen := &Generator{
		ModelRepo:     modelRepo,
		Token:         token,
		LoadFormat:    "auto",
		ConfigFormat:  "auto",
		TokenizerMode: "auto",
	}

	// Initialize default PresetParam
	gen.Param.Metadata.Name = modelNameSafe
	gen.Param.Metadata.ModelType = "tfs"
	gen.Param.Metadata.Version = fmt.Sprintf("%s/%s", HuggingFaceWebsite, modelRepo)
	gen.Param.Metadata.DownloadAtRuntime = true
	gen.Param.Metadata.DiskStorageRequirement = fmt.Sprintf("%dGi", SystemFileDiskSizeGiB)
	gen.Param.Metadata.ModelFileSize = "0Gi"

	return gen
}

func (g *Generator) getAuthHeader() string {
	if g.Token != "" {
		return "Bearer " + g.Token
	}
	if envToken := os.Getenv("HF_TOKEN"); envToken != "" {
		return "Bearer " + envToken
	}
	return ""
}

func (g *Generator) fetchURL(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	auth := g.getAuthHeader()
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		g.Param.Metadata.DownloadAuthRequired = true
		if auth == "" {
			return nil, fmt.Errorf("authentication required for accessing %s", url)
		}
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch %s: status %d", url, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

type FileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

func (g *Generator) FetchModelMetadata() error {
	files, err := g.listRepoFiles()
	if err != nil {
		return err
	}

	selectedFiles := g.selectWeightFiles(files)
	if len(selectedFiles) == 0 {
		return fmt.Errorf("no .safetensors or .bin files found")
	}

	if g.IsMistralModel {
		g.setMistralMode()
	}

	g.Param.Metadata.ModelFileSize = calculateModelFileSize(selectedFiles)
	g.Param.VLLM.ModelRunParams = make(map[string]string)

	if err := g.fetchAndParseConfig(); err != nil {
		return err
	}

	g.mergeTextConfig()
	return nil
}

// listRepoFiles fetches the full file tree for the model repo from HuggingFace.
func (g *Generator) listRepoFiles() ([]FileInfo, error) {
	url := fmt.Sprintf("%s/api/models/%s/tree/main?recursive=true", HuggingFaceWebsite, g.ModelRepo)
	body, err := g.fetchURL(url)
	if err != nil {
		return nil, fmt.Errorf("error listing files: %v", err)
	}

	var files []FileInfo
	if err := json.Unmarshal(body, &files); err != nil {
		return nil, fmt.Errorf("error parsing file list: %v", err)
	}
	return files, nil
}

// selectWeightFiles picks the model weight files to use and detects whether
// the model uses Mistral format. For Mistral-format models (those with
// consolidated*.safetensors), it sets g.IsMistralModel and returns only the
// consolidated files. For standard models, it prefers .safetensors over .bin
// when both are present.
func (g *Generator) selectWeightFiles(files []FileInfo) []FileInfo {
	var safetensors, bins, mistral []FileInfo

	for _, f := range files {
		if mistralRegex.MatchString(f.Path) {
			mistral = append(mistral, f)
		}
		if safetensorRegex.MatchString(f.Path) {
			safetensors = append(safetensors, f)
		} else if binRegex.MatchString(f.Path) {
			bins = append(bins, f)
		}
	}

	if len(mistral) > 0 {
		g.IsMistralModel = true
		return mistral
	}

	// Prefer safetensors over bin files when both exist.
	if len(safetensors) > 0 {
		return safetensors
	}
	return bins
}

func (g *Generator) setMistralMode() {
	g.LoadFormat = "mistral"
	g.ConfigFormat = "mistral"
	g.TokenizerMode = "mistral"
}

func calculateModelFileSize(files []FileInfo) string {
	var totalBytes int64
	for _, f := range files {
		totalBytes += f.Size
	}
	sizeGiB := float64(totalBytes) / (1024 * 1024 * 1024)
	return fmt.Sprintf("%.2fGi", sizeGiB)
}

// fetchAndParseConfig downloads and parses the model's config.json. For
// Mistral-format models, it falls back to params.json if config.json is absent.
func (g *Generator) fetchAndParseConfig() error {
	configBody, err := g.fetchConfigFile("config.json")
	if err != nil && g.IsMistralModel {
		// config.json not available; fall back to params.json (Mistral native format).
		configBody, err = g.fetchConfigFile("params.json")
	}
	if err != nil {
		return fmt.Errorf("error fetching config: %v", err)
	}

	configBody = sanitizeJSON(configBody)

	if err := json.Unmarshal(configBody, &g.ModelConfig); err != nil {
		return fmt.Errorf("error parsing config: %v", err)
	}
	return nil
}

// vLLM delegates the loading of config.json to HuggingFace transformer library
// (https://github.com/huggingface/transformers/blob/main/src/transformers/configuration_utils.py#L552).
// The library uses Python's json.loads which accepts non-standard JSON literals (e.g. Infinity, NaN).
// However, Go's standard library encoding/json only supports standard JSON values. sanitizeJSON replaces
// non-standard JSON literals (Infinity, -Infinity, NaN) with null so the data can be parsed by encoding/json.
func sanitizeJSON(data []byte) []byte {
	// Replace standalone Infinity, -Infinity, NaN with null
	re := regexp.MustCompile(`(?:(?:^|[,\[:\s])\s*)-?Infinity|(?:(?:^|[,\[:\s])\s*)NaN`)
	return re.ReplaceAllFunc(data, func(match []byte) []byte {
		// Preserve the prefix (comma, bracket, colon, whitespace) before the value
		trimmed := strings.TrimLeft(string(match), " \t\n\r,[:") //nolint:gocritic
		prefix := string(match[:len(match)-len(trimmed)])
		return []byte(prefix + "null")
	})
}

func (g *Generator) fetchConfigFile(name string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/resolve/main/%s", HuggingFaceWebsite, g.ModelRepo, name)
	return g.fetchURL(url)
}

// mergeTextConfig promotes fields from a nested "text_config" or "llm_config"
// object into the top-level config. This is needed for multimodal models
// (e.g., Gemma-3, Ministral-3, Nemotron-VL) where architecture-specific
// parameters live under a nested config key.
func (g *Generator) mergeTextConfig() {
	var nested map[string]interface{}
	if tc, ok := g.ModelConfig["text_config"].(map[string]interface{}); ok {
		nested = tc
	} else if lc, ok := g.ModelConfig["llm_config"].(map[string]interface{}); ok {
		nested = lc
	}
	if nested == nil {
		return
	}
	for k, v := range nested {
		if _, exists := g.ModelConfig[k]; !exists {
			g.ModelConfig[k] = v
		}
	}
}

func getInt(config map[string]interface{}, keys []string, defaultVal int) int {
	for _, key := range keys {
		if val, ok := config[key]; ok {
			switch v := val.(type) {
			case float64:
				return int(v)
			case int:
				return v
			case string:
				if i, err := strconv.Atoi(v); err == nil {
					return i
				}
			}
		}
	}
	return defaultVal
}

func (g *Generator) ParseModelMetadata() {
	maxPos := getInt(g.ModelConfig, configKeyMap["modelTokenLimit"], DefaultModelTokenLimit)

	g.Param.Metadata.ModelTokenLimit = maxPos

	g.Param.Metadata.Architectures = []string{}
	if arch, ok := g.ModelConfig["architectures"].([]interface{}); ok {
		for _, a := range arch {
			if archStr, ok := a.(string); ok {
				g.Param.Metadata.Architectures = append(g.Param.Metadata.Architectures, archStr)
			}
		}
	}

	// Override architectures for specific model families only when none were parsed
	if len(g.Param.Metadata.Architectures) == 0 {
		if strings.HasPrefix(g.Param.Metadata.Name, "mistral-large-3") {
			g.Param.Metadata.Architectures = []string{"MistralLarge3ForCausalLM"}
		} else if strings.HasPrefix(g.Param.Metadata.Name, "ministral-3") {
			g.Param.Metadata.Architectures = []string{"Mistral3ForConditionalGeneration"}
		}
	}

	// set reasoning parser based on model name prefix
	for prefix, parser := range reasoningParserModeNamePrefixMap {
		if strings.HasPrefix(g.Param.Metadata.Name, prefix) {
			g.Param.Metadata.ReasoningParser = parser
			break
		}
	}

	// set reasoning parser based on model architecture if not set by name prefix
	if g.Param.Metadata.ReasoningParser == "" {
		for _, arch := range g.Param.Metadata.Architectures {
			if parser, ok := reasoningParserArchMap[arch]; ok {
				g.Param.Metadata.ReasoningParser = parser
				break
			}
		}
	}

	// set ToolCallParser based on model name prefix
	// sort the keys of toolCallParserModeNamePrefixMap in reverse alphabetical order and then iterate
	// this is to ensure that longer (more specific) prefixes are matched first
	prefixes := make([]string, 0, len(toolCallParserModeNamePrefixMap))
	for prefix := range toolCallParserModeNamePrefixMap {
		prefixes = append(prefixes, prefix)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(prefixes)))

	for _, prefix := range prefixes {
		if strings.HasPrefix(g.Param.Metadata.Name, prefix) {
			g.Param.Metadata.ToolCallParser = toolCallParserModeNamePrefixMap[prefix]
			break
		}
	}

	// set ToolCallParser based on model architecture if not set by name prefix
	if g.Param.Metadata.ToolCallParser == "" {
		for _, arch := range g.Param.Metadata.Architectures {
			if parser, ok := toolCallParserArchMap[arch]; ok {
				g.Param.Metadata.ToolCallParser = parser
				break
			}
		}
	}

	// set ChatTemplate based on model name prefix
	for prefix, template := range chatTemplatePrefixMap {
		if strings.HasPrefix(g.Param.Metadata.Name, prefix) {
			g.Param.Metadata.ChatTemplate = template
			break
		}
	}

	// Parse quantization config (e.g., AWQ, GPTQ) from HuggingFace config.json.
	if qc, ok := g.ModelConfig["quantization_config"].(map[string]interface{}); ok {
		if qm, ok := qc["quant_method"].(string); ok {
			g.Param.Metadata.QuantMethod = qm
		}
		g.Param.Metadata.QuantBits = getInt(qc, []string{"bits"}, 0)
	}
}

func (g *Generator) calculateStorageSize() string {
	szStr := strings.TrimSuffix(g.Param.Metadata.ModelFileSize, "Gi")
	sz, _ := strconv.ParseFloat(szStr, 64)
	req := int(sz + SystemFileDiskSizeGiB)
	return fmt.Sprintf("%dGi", req)
}

func (g *Generator) calculateKVCacheTokenSize() (int, string) {
	config := g.ModelConfig

	hiddenSize := getInt(config, configKeyMap["hiddenSize"], 0)
	hiddenLayers := getInt(config, configKeyMap["numHiddenLayers"], 0)
	attentionHeads := getInt(config, configKeyMap["numAttentionHeads"], 0)
	kvHeads := getInt(config, configKeyMap["numKeyValueHeads"], 0)
	headDim := getInt(config, optionalKeyMap["headDim"], 0)

	if headDim == 0 && attentionHeads > 0 {
		headDim = hiddenSize / attentionHeads
	}

	// DeepSeek MLA
	kvLoraRank := getInt(config, optionalKeyMap["kvLoraRank"], -1)
	qkRopeHeadDim := getInt(config, optionalKeyMap["qkRopeHeadDim"], 0)

	// Fallback KV heads
	if kvHeads == 0 && attentionHeads > 0 {
		if mq, ok := config["multi_query"].(bool); ok && mq {
			kvHeads = 1
		} else {
			kvHeads = attentionHeads
		}
	}

	attnType := "Unknown"
	elementsPerToken := 0

	if kvLoraRank != -1 {
		attnType = "MLA"
		elementsPerToken = kvLoraRank + qkRopeHeadDim
	} else if attentionHeads > 0 && kvHeads > 0 && headDim > 0 {
		elementsPerToken = 2 * kvHeads * headDim

		if attentionHeads == kvHeads {
			attnType = "MHA"
		} else if kvHeads == 1 {
			attnType = "MQA"
		} else {
			attnType = "GQA"
		}
	}

	totalElements := elementsPerToken * hiddenLayers
	// TODO: honor kv-cache quantization instead of hardcoding fp16
	tokenSize := totalElements * 2 // fp16

	return tokenSize, attnType
}

func (g *Generator) FinalizeParams() {
	g.Param.Metadata.DiskStorageRequirement = g.calculateStorageSize()

	// VLLM Params
	if g.Param.VLLM.ModelRunParams == nil {
		g.Param.VLLM.ModelRunParams = make(map[string]string)
	}
	g.Param.VLLM.ModelName = g.Param.Name
	g.Param.VLLM.ModelRunParams["load_format"] = g.LoadFormat
	g.Param.VLLM.ModelRunParams["config_format"] = g.ConfigFormat
	g.Param.VLLM.ModelRunParams["tokenizer_mode"] = g.TokenizerMode

	// Set attention backend based on model name prefix
	for prefix, backend := range attentionBackendPrefixMap {
		if strings.HasPrefix(g.Param.Metadata.Name, prefix) {
			g.Param.VLLM.ModelRunParams["attention-backend"] = backend
			break
		}
	}

	bpt, attnType := g.calculateKVCacheTokenSize()
	g.Param.Metadata.BytesPerToken = bpt
	g.Param.AttnType = attnType
}

// loadFromCatalog checks whether the model repo exists in the embedded catalog.
// If found, it populates the generator's ModelConfig and Param fields from the
// catalog entry, avoiding any HuggingFace API calls.
func (g *Generator) loadFromCatalog() bool {
	if len(g.CatalogData) == 0 {
		return false
	}

	catalog := ModelCatalog{}
	if err := yaml.Unmarshal(g.CatalogData, &catalog); err != nil {
		fmt.Fprintf(os.Stderr, "failed to unmarshal model catalog for %q: %v\n", g.ModelRepo, err)
		return false
	}

	var entry *CatalogEntry
	for i, m := range catalog.Models {
		if strings.EqualFold(m.Name, g.ModelRepo) {
			entry = &catalog.Models[i]
			break
		}
	}
	if entry == nil {
		return false
	}

	// Populate ModelConfig from catalog entry so existing calculation
	// functions (ParseModelMetadata, FinalizeParams) work unchanged.
	g.ModelConfig = map[string]interface{}{
		"hidden_size":             entry.HiddenSize,
		"num_hidden_layers":       entry.NumHiddenLayers,
		"num_attention_heads":     entry.NumAttentionHeads,
		"num_key_value_heads":     entry.NumKeyValueHeads,
		"max_position_embeddings": entry.ModelTokenLimit,
	}
	if entry.HeadDim > 0 {
		g.ModelConfig["head_dim"] = entry.HeadDim
	}
	if entry.KVLoraRank > 0 {
		g.ModelConfig["kv_lora_rank"] = entry.KVLoraRank
	}
	if entry.QKRopeHeadDim > 0 {
		g.ModelConfig["qk_rope_head_dim"] = entry.QKRopeHeadDim
	}

	// Restore quantization_config so ParseModelMetadata can pick it up.
	if entry.QuantMethod != "" {
		g.ModelConfig["quantization_config"] = map[string]interface{}{
			"quant_method": entry.QuantMethod,
			"bits":         entry.QuantBits,
		}
	}

	// Set architectures in config for ParseModelMetadata to pick up
	archInterfaces := make([]interface{}, len(entry.Architectures))
	for i, a := range entry.Architectures {
		archInterfaces[i] = a
	}
	g.ModelConfig["architectures"] = archInterfaces

	// Populate fields that FetchModelMetadata would have set
	g.Param.Metadata.ModelFileSize = entry.ModelFileSize
	g.Param.VLLM.ModelRunParams = make(map[string]string)

	if entry.LoadFormat != "" {
		g.LoadFormat = entry.LoadFormat
	}
	if entry.ConfigFormat != "" {
		g.ConfigFormat = entry.ConfigFormat
	} else if entry.LoadFormat != "" {
		g.ConfigFormat = entry.LoadFormat
	}
	if entry.TokenizerMode != "" {
		g.TokenizerMode = entry.TokenizerMode
	} else if entry.LoadFormat != "" {
		g.TokenizerMode = entry.LoadFormat
	}

	return true
}

func (g *Generator) Generate() (*model.PresetParam, error) {
	if !g.loadFromCatalog() {
		if err := g.FetchModelMetadata(); err != nil {
			return nil, err
		}
	}
	g.ParseModelMetadata()
	g.FinalizeParams()

	return &g.Param, nil
}

// GeneratePreset is the global function to generate preset param.
// If catalogData is provided, the generator will check for the model in the
// catalog before making any HuggingFace API calls.
func GeneratePreset(modelRepo, token string, catalogData ...[]byte) (*model.PresetParam, error) {
	if modelRepo == "" {
		return nil, errors.New("model repo is required")
	}
	gen := NewGenerator(modelRepo, token)
	if len(catalogData) > 0 {
		gen.CatalogData = catalogData[0]
	}
	return gen.Generate()
}
