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
	"fmt"
	"math"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
)

// CatalogEntry represents a pre-computed model entry in the catalog,
// storing the raw HuggingFace config values needed for preset generation
// without requiring runtime API calls.
type CatalogEntry struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	License      string   `yaml:"license,omitempty"`
	PipelineTag       string   `yaml:"pipelineTag,omitempty"`
	BaseModel         []string `yaml:"baseModel,omitempty"`
	ModelFileSize     string   `yaml:"modelFileSize"`
	Architectures     []string `yaml:"architectures"`
	ModelTokenLimit   int      `yaml:"modelTokenLimit"`
	HiddenSize        int      `yaml:"hiddenSize"`
	NumHiddenLayers   int      `yaml:"numHiddenLayers"`
	NumAttentionHeads int      `yaml:"numAttentionHeads"`
	NumKeyValueHeads  int      `yaml:"numKeyValueHeads"`
	LoadFormat        string   `yaml:"loadFormat,omitempty"`
	ConfigFormat      string   `yaml:"configFormat,omitempty"`
	TokenizerMode     string   `yaml:"tokenizerMode,omitempty"`
	HeadDim           int      `yaml:"headDim,omitempty"`
	KVLoraRank        int      `yaml:"kvLoraRank,omitempty"`
	QKRopeHeadDim     int      `yaml:"qkRopeHeadDim,omitempty"`
}

// ModelCatalog holds the list of pre-computed model entries.
type ModelCatalog struct {
	Models []CatalogEntry `yaml:"models"`
}

// configKeyMap maps catalog field names to the ordered list of HuggingFace
// config keys to try, mirroring the getInt lookup order.
var configKeyMap = map[string][]string{
	"modelTokenLimit":   {"max_position_embeddings", "n_ctx", "seq_length", "max_seq_len", "max_sequence_length"},
	"hiddenSize":        {"hidden_size", "n_embd", "d_model"},
	"numHiddenLayers":   {"num_hidden_layers", "n_layer", "n_layers"},
	"numAttentionHeads": {"num_attention_heads", "n_head", "n_heads"},
	"numKeyValueHeads":  {"num_key_value_heads", "n_head_kv", "n_kv_heads"},
}

// optionalKeyMap holds catalog fields that are only stored when present and > 0.
var optionalKeyMap = map[string][]string{
	"headDim":       {"head_dim"},
	"kvLoraRank":    {"kv_lora_rank"},
	"qkRopeHeadDim": {"qk_rope_head_dim"},
}

// fetchModelInfo fetches the model info from the HuggingFace API
// and returns license, pipeline_tag, and base_model.
func fetchModelInfo(g *Generator, repo string) (license, pipelineTag string, baseModel []string) {
	url := fmt.Sprintf("%s/api/models/%s", HuggingFaceWebsite, repo)
	body, err := g.fetchURL(url)
	if err != nil {
		return
	}

	var info map[string]interface{}
	if err := json.Unmarshal(body, &info); err != nil {
		return
	}

	// pipeline_tag: prefer top-level, fall back to cardData
	if pt, ok := info["pipeline_tag"].(string); ok && pt != "" {
		pipelineTag = pt
	}

	// cardData holds license and base_model
	cardData, _ := info["cardData"].(map[string]interface{})
	if cardData != nil {
		if l, ok := cardData["license"].(string); ok {
			license = l
		}
		if pipelineTag == "" {
			if pt, ok := cardData["pipeline_tag"].(string); ok {
				pipelineTag = pt
			}
		}
		// base_model can be a string or a list of strings
		switch bm := cardData["base_model"].(type) {
		case string:
			if bm != "" {
				baseModel = []string{bm}
			}
		case []interface{}:
			for _, v := range bm {
				if s, ok := v.(string); ok && s != "" {
					baseModel = append(baseModel, s)
				}
			}
		}
	}

	return
}

// FetchCatalogEntry fetches a CatalogEntry for a model repo from HuggingFace.
func FetchCatalogEntry(repo, token string) (*CatalogEntry, error) {
	g := NewGenerator(repo, token)

	// Fetch model info (license, pipeline, base_model)
	license, pipelineTag, baseModel := fetchModelInfo(g, repo)

	// Fetch file listing
	url := fmt.Sprintf("%s/api/models/%s/tree/main?recursive=true", HuggingFaceWebsite, repo)
	body, err := g.fetchURL(url)
	if err != nil {
		return nil, fmt.Errorf("error listing files: %v", err)
	}

	var files []FileInfo
	if err := json.Unmarshal(body, &files); err != nil {
		return nil, fmt.Errorf("error parsing file list: %v", err)
	}

	var selectedFiles, mistralFiles []FileInfo
	for _, f := range files {
		if mistralRegex.MatchString(f.Path) {
			mistralFiles = append(mistralFiles, f)
		}
		if safetensorRegex.MatchString(f.Path) || binRegex.MatchString(f.Path) {
			selectedFiles = append(selectedFiles, f)
		}
	}

	var configFile string
	isMistral := len(mistralFiles) > 0

	if isMistral {
		selectedFiles = mistralFiles
		configFile = "params.json"
	} else if len(selectedFiles) > 0 {
		configFile = "config.json"
		hasSafetensors := false
		for _, f := range selectedFiles {
			if strings.HasSuffix(f.Path, ".safetensors") {
				hasSafetensors = true
				break
			}
		}
		if hasSafetensors {
			var only []FileInfo
			for _, f := range selectedFiles {
				if strings.HasSuffix(f.Path, ".safetensors") {
					only = append(only, f)
				}
			}
			selectedFiles = only
		}
	} else {
		return nil, fmt.Errorf("no .safetensors or .bin files found")
	}

	var totalBytes int64
	for _, f := range selectedFiles {
		totalBytes += f.Size
	}
	sizeGiB := int(math.Ceil(float64(totalBytes) / (1024 * 1024 * 1024)))

	configURL := fmt.Sprintf("%s/%s/resolve/main/%s", HuggingFaceWebsite, repo, configFile)
	configBody, err := g.fetchURL(configURL)
	if err != nil {
		return nil, fmt.Errorf("error fetching config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(configBody, &config); err != nil {
		return nil, fmt.Errorf("error parsing config: %v", err)
	}

	entry := &CatalogEntry{
		Name:        repo,
		Description: fmt.Sprintf("%s/%s", HuggingFaceWebsite, repo),
		License:      license,
		PipelineTag:  pipelineTag,
		BaseModel:    baseModel,
		ModelFileSize: fmt.Sprintf("%dGi", sizeGiB),
	}

	if archList, ok := config["architectures"].([]interface{}); ok {
		for _, a := range archList {
			if s, ok := a.(string); ok {
				entry.Architectures = append(entry.Architectures, s)
			}
		}
	}

	entry.ModelTokenLimit = getInt(config, configKeyMap["modelTokenLimit"], 0)
	entry.HiddenSize = getInt(config, configKeyMap["hiddenSize"], 0)
	entry.NumHiddenLayers = getInt(config, configKeyMap["numHiddenLayers"], 0)
	entry.NumAttentionHeads = getInt(config, configKeyMap["numAttentionHeads"], 0)
	entry.NumKeyValueHeads = getInt(config, configKeyMap["numKeyValueHeads"], 0)

	entry.HeadDim = getInt(config, optionalKeyMap["headDim"], 0)
	entry.KVLoraRank = getInt(config, optionalKeyMap["kvLoraRank"], 0)
	entry.QKRopeHeadDim = getInt(config, optionalKeyMap["qkRopeHeadDim"], 0)

	if entry.HeadDim > 0 && entry.NumAttentionHeads > 0 && entry.HiddenSize > 0 {
		if entry.HeadDim == entry.HiddenSize/entry.NumAttentionHeads {
			entry.HeadDim = 0
		}
	}

	if isMistral {
		entry.LoadFormat = "mistral"
		entry.ConfigFormat = "mistral"
		entry.TokenizerMode = "mistral"
	}

	return entry, nil
}

// LoadCatalog reads a model_catalog.yaml file and returns its entries.
func LoadCatalog(path string) ([]CatalogEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var catalog ModelCatalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("error parsing catalog: %v", err)
	}
	return catalog.Models, nil
}

// SaveCatalog writes catalog entries to a YAML file.
func SaveCatalog(path string, entries []CatalogEntry) error {
	catalog := ModelCatalog{Models: entries}
	data, err := yaml.Marshal(&catalog)
	if err != nil {
		return fmt.Errorf("error marshaling catalog: %v", err)
	}
	return os.WriteFile(path, data, 0600)
}
