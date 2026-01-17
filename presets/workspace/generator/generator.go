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
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kaito-project/kaito/pkg/model"
)

const (
	SystemFileDiskSizeGiB  = 50
	DefaultModelTokenLimit = 2048
)

var (
	safetensorRegex = regexp.MustCompile(`.*\.safetensors`)
	binRegex        = regexp.MustCompile(`.*\.bin`)
	mistralRegex    = regexp.MustCompile(`consolidated.*\.safetensors`)
)

type Generator struct {
	ModelRepo string
	Token     string
	Param     model.PresetParam
	Config    map[string]interface{}

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
	gen.Param.Name = modelNameSafe
	gen.Param.ModelType = "tfs"
	gen.Param.Version = "0.0.1"
	gen.Param.DownloadAtRuntime = true
	gen.Param.DiskStorageRequirement = "50Gi"
	gen.Param.ModelFileSize = "0Gi"

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
		g.Param.DownloadAuthRequired = true
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
	// List files using HF API
	url := fmt.Sprintf("https://huggingface.co/api/models/%s/tree/main?recursive=true", g.ModelRepo)
	body, err := g.fetchURL(url)
	if err != nil {
		return fmt.Errorf("error listing files: %v", err)
	}

	var files []FileInfo
	if err := json.Unmarshal(body, &files); err != nil {
		return fmt.Errorf("error parsing file list: %v", err)
	}

	// Filter files
	var selectedFiles []FileInfo
	var mistralFiles []FileInfo

	for _, f := range files {
		if mistralRegex.MatchString(f.Path) {
			mistralFiles = append(mistralFiles, f)
		}
		if safetensorRegex.MatchString(f.Path) || binRegex.MatchString(f.Path) {
			selectedFiles = append(selectedFiles, f)
		}
	}

	var configFile string

	// Logic to detect model format
	if len(mistralFiles) > 0 {
		g.LoadFormat = "mistral"
		g.ConfigFormat = "mistral"
		g.TokenizerMode = "mistral"
		configFile = "params.json"
		selectedFiles = mistralFiles
	} else if len(selectedFiles) > 0 {
		configFile = "config.json"

		// Prefer safetensors if mixed with bin
		hasSafetensors := false
		for _, f := range selectedFiles {
			if strings.HasSuffix(f.Path, ".safetensors") {
				hasSafetensors = true
				break
			}
		}

		if hasSafetensors {
			var onlySafetensors []FileInfo
			for _, f := range selectedFiles {
				if strings.HasSuffix(f.Path, ".safetensors") {
					onlySafetensors = append(onlySafetensors, f)
				}
			}
			selectedFiles = onlySafetensors
		}
	} else {
		return fmt.Errorf("no .safetensors or .bin files found")
	}

	var totalBytes int64
	for _, f := range selectedFiles {
		totalBytes += f.Size
	}

	modelSizeGB := float64(totalBytes) / (1024 * 1024 * 1024)
	g.Param.ModelFileSize = fmt.Sprintf("%.0fGi", math.Ceil(modelSizeGB))

	g.Param.VLLM.ModelRunParams = make(map[string]string)

	configURL := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", g.ModelRepo, configFile)
	configBody, err := g.fetchURL(configURL)
	if err != nil {
		return fmt.Errorf("error fetching config: %v", err)
	}

	if err := json.Unmarshal(configBody, &g.ModelConfig); err != nil {
		return fmt.Errorf("error parsing config: %v", err)
	}

	return nil
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
	maxPos := getInt(g.ModelConfig, []string{
		"max_position_embeddings",
		"n_ctx",
		"seq_length",
		"max_seq_len",
		"max_sequence_length",
	}, DefaultModelTokenLimit)

	g.Param.ModelTokenLimit = maxPos
}

func (g *Generator) calculateStorageSize() string {
	szStr := strings.TrimSuffix(g.Param.ModelFileSize, "Gi")
	sz, _ := strconv.ParseFloat(szStr, 64)
	req := int(sz + SystemFileDiskSizeGiB)
	return fmt.Sprintf("%dGi", req)
}

func (g *Generator) calculateKVCacheTokenSize() (int, string) {
	config := g.ModelConfig

	hiddenSize := getInt(config, []string{"hidden_size", "n_embd", "d_model"}, 0)
	hiddenLayers := getInt(config, []string{"num_hidden_layers", "n_layer", "n_layers"}, 0)
	attentionHeads := getInt(config, []string{"num_attention_heads", "n_head", "n_heads"}, 0)
	kvHeads := getInt(config, []string{"num_key_value_heads", "n_head_kv", "n_kv_heads"}, 0)
	headDim := getInt(config, []string{"head_dim"}, 0)

	if headDim == 0 && attentionHeads > 0 {
		headDim = hiddenSize / attentionHeads
	}

	// DeepSeek MLA
	kvLoraRank := getInt(config, []string{"kv_lora_rank"}, -1)
	qkRopeHeadDim := getInt(config, []string{"qk_rope_head_dim"}, 0)

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
	tokenSize := totalElements * 2 // fp16

	return tokenSize, attnType
}

func (g *Generator) FinalizeParams() {
	g.Param.DiskStorageRequirement = g.calculateStorageSize()

	// VLLM Params
	if g.Param.VLLM.ModelRunParams == nil {
		g.Param.VLLM.ModelRunParams = make(map[string]string)
	}
	g.Param.VLLM.ModelName = g.Param.Name
	g.Param.VLLM.ModelRunParams["load_format"] = g.LoadFormat
	g.Param.VLLM.ModelRunParams["config_format"] = g.ConfigFormat
	g.Param.VLLM.ModelRunParams["tokenizer_mode"] = g.TokenizerMode

	bpt, attnType := g.calculateKVCacheTokenSize()
	g.Param.BytesPerToken = bpt
	g.Param.AttnType = attnType
}

func (g *Generator) Generate() (*model.PresetParam, error) {
	if err := g.FetchModelMetadata(); err != nil {
		return nil, err
	}
	g.ParseModelMetadata()
	g.FinalizeParams()

	return &g.Param, nil
}

// GeneratePreset is the global function to generate preset param
func GeneratePreset(modelRepo, token string) (*model.PresetParam, error) {
	if modelRepo == "" {
		return nil, errors.New("model repo is required")
	}
	gen := NewGenerator(modelRepo, token)
	return gen.Generate()
}
