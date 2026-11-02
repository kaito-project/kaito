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
	"strconv"
	"testing"

	"github.com/kaito-project/kaito/pkg/model"
)

func TestSpeculativeDecodingByPresetEntries(t *testing.T) {
	tests := []struct {
		repoKey   string
		wantUser  string
		wantModel string
	}{
		{"deepseek-ai/deepseek-r1-0528", "deepseek-r1-0528", ""},
		{"deepseek-ai/deepseek-v3-0324", "deepseek-v3-0324", ""},
		{"deepseek-ai/deepseek-v3.2", "deepseek-ai/DeepSeek-V3.2", ""},
		{"zai-org/glm-5.2-fp8", "zai-org/GLM-5.2-FP8", ""},
		{"nvidia/deepseek-v4-flash-nvfp4", "nvidia/DeepSeek-V4-Flash-NVFP4", ""},
		{"xiaomimimo/mimo-7b-base", "XiaomiMiMo/MiMo-7B-Base", ""},
		{"qwen/qwen3.5-2b", "Qwen/Qwen3.5-2B", ""},
		{"qwen/qwen3.5-4b", "Qwen/Qwen3.5-4B", ""},
		{"qwen/qwen3.5-9b", "Qwen/Qwen3.5-9B", ""},
		{"qwen/qwen3.5-122b-a10b-gptq-int4", "Qwen/Qwen3.5-122B-A10B-GPTQ-Int4", ""},
		{"qwen/qwen3.5-122b-a10b", "Qwen/Qwen3.5-122B-A10B", ""},
		{"qwen/qwen3.6-35b-a3b-fp8", "Qwen/Qwen3.6-35B-A3B-FP8", ""},
		{"qwen/qwen3.6-35b-a3b", "Qwen/Qwen3.6-35B-A3B", ""},
		{"qwen/qwen3.6-27b", "Qwen/Qwen3.6-27B", ""},
		{"qwen/qwen3.5-397b-a17b-gptq-int4", "Qwen/Qwen3.5-397B-A17B-GPTQ-Int4", ""},
	}

	for _, tc := range tests {
		entry, ok := speculativeDecodingByPreset[tc.repoKey]
		if !ok {
			t.Errorf("missing entry for %q", tc.repoKey)
			continue
		}
		if entry.UserFacing != tc.wantUser {
			t.Errorf("entry %q: UserFacing = %q, want %q", tc.repoKey, entry.UserFacing, tc.wantUser)
		}
		if entry.Config == nil {
			t.Errorf("entry %q: Config is nil", tc.repoKey)
			continue
		}
		if entry.Config.Method != "mtp" {
			t.Errorf("entry %q: Method = %q, want mtp", tc.repoKey, entry.Config.Method)
		}
		if entry.Config.MTP == nil {
			t.Errorf("entry %q: MTP is nil", tc.repoKey)
			continue
		}
		if entry.Config.MTP.NumSpeculativeTokens != 1 {
			t.Errorf("entry %q: NumSpeculativeTokens = %d, want 1", tc.repoKey, entry.Config.MTP.NumSpeculativeTokens)
		}
		if entry.Config.MTP.Model != tc.wantModel {
			t.Errorf("entry %q: Model = %q, want %q", tc.repoKey, entry.Config.MTP.Model, tc.wantModel)
		}
	}
}

func TestMTPDraftModelEntryIncludesModel(t *testing.T) {
	entry := mtpSpecDecoEntryWithModel("example/main-model", "example/draft-model")
	if entry.Config == nil || entry.Config.MTP == nil {
		t.Fatal("mtp draft-model entry missing MTP config")
	}
	if entry.Config.MTP.Model != "example/draft-model" {
		t.Fatalf("draft model = %q, want example/draft-model", entry.Config.MTP.Model)
	}
}

func TestSpeculativeDecodingUnknownPresetStaysNil(t *testing.T) {
	_, ok := speculativeDecodingByPreset["unknown/model"]
	if ok {
		t.Error("unknown model should not be in speculativeDecodingByPreset")
	}
}

func TestSpeculativeDecodingConfigConsistency(t *testing.T) {
	// Validate that each entry has exactly one non-nil sub-config matching Method
	for key, entry := range speculativeDecodingByPreset {
		cfg := entry.Config
		if cfg == nil {
			t.Errorf("%s: Config is nil", key)
			continue
		}
		switch cfg.Method {
		case "mtp":
			if cfg.MTP == nil {
				t.Errorf("%s: method=mtp but MTP is nil", key)
			}
			if cfg.NGram != nil {
				t.Errorf("%s: method=mtp but NGram is non-nil", key)
			}
			if cfg.MTP != nil && cfg.MTP.NumSpeculativeTokens <= 0 {
				t.Errorf("%s: mtp.NumSpeculativeTokens must be > 0", key)
			}
		case "ngram":
			if cfg.NGram == nil {
				t.Errorf("%s: method=ngram but NGram is nil", key)
			}
		case "dspark":
			if cfg.DSpark == nil {
				t.Errorf("%s: method=dspark but DSpark is nil", key)
				continue
			}
			if cfg.MTP != nil || cfg.NGram != nil {
				t.Errorf("%s: method=dspark but another sub-config is non-nil", key)
			}
			if cfg.DSpark.NumSpeculativeTokens <= 0 {
				t.Errorf("%s: dspark.NumSpeculativeTokens must be > 0", key)
			}
		default:
			t.Errorf("%s: unsupported method %q", key, cfg.Method)
		}
	}
}

func TestDeepCopySpeculativeDecoding(t *testing.T) {
	p := &model.PresetParam{
		SpeculativeDecoding: &model.SpeculativeDecodingConfig{
			Method: "mtp",
			MTP: &model.MTPConfig{
				NumSpeculativeTokens: 1,
				Model:                "example/draft-model",
			},
		},
	}
	c := p.DeepCopy()
	if c.SpeculativeDecoding == nil {
		t.Fatal("DeepCopy: SpeculativeDecoding is nil")
	}
	if c.SpeculativeDecoding == p.SpeculativeDecoding {
		t.Fatal("DeepCopy: SpeculativeDecoding pointer not cloned")
	}
	if c.SpeculativeDecoding.MTP == p.SpeculativeDecoding.MTP {
		t.Fatal("DeepCopy: MTP pointer not cloned")
	}
	if c.SpeculativeDecoding.MTP.NumSpeculativeTokens != 1 {
		t.Fatalf("DeepCopy: NumSpeculativeTokens = %d, want 1", c.SpeculativeDecoding.MTP.NumSpeculativeTokens)
	}
	if c.SpeculativeDecoding.MTP.Model != "example/draft-model" {
		t.Fatalf("DeepCopy: Model = %q, want example/draft-model", c.SpeculativeDecoding.MTP.Model)
	}

	// Mutate copy, original should be unaffected
	c.SpeculativeDecoding.MTP.NumSpeculativeTokens = 5
	c.SpeculativeDecoding.MTP.Model = "example/draft-model-v2"
	if p.SpeculativeDecoding.MTP.NumSpeculativeTokens != 1 {
		t.Fatal("DeepCopy: mutation leaked to original")
	}
	if p.SpeculativeDecoding.MTP.Model != "example/draft-model" {
		t.Fatal("DeepCopy: model mutation leaked to original")
	}
}

func TestDSparkSpecDecoEntryDeepSeekV41Flash1017(t *testing.T) {
	entry, ok := speculativeDecodingByPreset["deepseek-ai/deepseek-v4.1-flash-1017"]
	if !ok {
		t.Fatal("missing dspark entry for deepseek-ai/deepseek-v4.1-flash-1017")
	}
	if entry.UserFacing != "deepseek-ai/DeepSeek-V4.1-Flash-1017" {
		t.Errorf("UserFacing = %q", entry.UserFacing)
	}
	cfg := entry.Config
	if cfg == nil || cfg.Method != SpeculativeDecodingMethodDSpark || cfg.DSpark == nil {
		t.Fatalf("want dspark config, got %+v", cfg)
	}
	want := model.DSparkConfig{
		NumSpeculativeTokens:       5,
		DraftSampleMethod:          "probabilistic",
		RejectionSampleMethod:      "block",
		EnableAdaptiveVerification: true,
	}
	if *cfg.DSpark != want {
		t.Errorf("DSpark = %+v, want %+v", *cfg.DSpark, want)
	}
	wantArgs := map[string]string{
		"max-num-batched-tokens":     "4096",
		"max-num-seqs":               "256",
		"max-cudagraph-capture-size": "1024",
	}
	if len(cfg.EngineArgs) != len(wantArgs) {
		t.Errorf("EngineArgs = %v, want %v", cfg.EngineArgs, wantArgs)
	}
	for k, v := range wantArgs {
		if cfg.EngineArgs[k] != v {
			t.Errorf("EngineArgs[%q] = %q, want %q", k, cfg.EngineArgs[k], v)
		}
	}
}

// TestDSparkEngineArgsFitDraftSlots guards the vLLM startup crash where the
// profiling run indexes max-num-seqs x num_speculative_tokens draft slots into
// an input buffer sized by max-num-batched-tokens.
func TestDSparkEngineArgsFitDraftSlots(t *testing.T) {
	for key, entry := range speculativeDecodingByPreset {
		cfg := entry.Config
		if cfg == nil || cfg.DSpark == nil {
			continue
		}
		seqs, seqsErr := strconv.Atoi(cfg.EngineArgs["max-num-seqs"])
		tokens, tokensErr := strconv.Atoi(cfg.EngineArgs["max-num-batched-tokens"])
		if tokensErr != nil {
			continue
		}
		if seqsErr != nil {
			t.Errorf("%s: max-num-batched-tokens set without max-num-seqs; vLLM's default sequence count can overflow the draft slots", key)
			continue
		}
		if seqs*cfg.DSpark.NumSpeculativeTokens > tokens {
			t.Errorf("%s: max-num-seqs (%d) x num_speculative_tokens (%d) exceeds max-num-batched-tokens (%d)",
				key, seqs, cfg.DSpark.NumSpeculativeTokens, tokens)
		}
	}
}

func TestDeepCopySpeculativeDecodingDSpark(t *testing.T) {
	p := &model.PresetParam{SpeculativeDecoding: speculativeDecodingByPreset["deepseek-ai/deepseek-v4.1-flash-1017"].Config}
	c := p.DeepCopy()
	c.SpeculativeDecoding.DSpark.EnableAdaptiveVerification = false
	c.SpeculativeDecoding.EngineArgs["max-num-seqs"] = "1"
	if !p.SpeculativeDecoding.DSpark.EnableAdaptiveVerification || p.SpeculativeDecoding.EngineArgs["max-num-seqs"] != "256" {
		t.Fatal("DeepCopy: mutation leaked into the shared preset entry")
	}
}

func TestDSparkSpecDecoEntryUsesGivenConfig(t *testing.T) {
	// DeepSeek-V4-Flash's recipe drafts 7 tokens without block rejection or
	// adaptive verification: https://recipes.vllm.ai/deepseek-ai/DeepSeek-V4-Flash
	v4Flash := model.DSparkConfig{NumSpeculativeTokens: 7, DraftSampleMethod: "probabilistic"}
	entry := dsparkSpecDecoEntry("example/model", v4Flash, nil)
	if entry.Config.Method != SpeculativeDecodingMethodDSpark {
		t.Fatalf("Method = %q", entry.Config.Method)
	}
	if *entry.Config.DSpark != v4Flash {
		t.Errorf("DSpark = %+v, want %+v", *entry.Config.DSpark, v4Flash)
	}

	other := dsparkSpecDecoEntry("example/other", v4Flash, nil)
	other.Config.DSpark.NumSpeculativeTokens = 3
	if entry.Config.DSpark.NumSpeculativeTokens != 7 {
		t.Error("entries built from the same config share one DSparkConfig")
	}
	if dsparkDeepSeekV41Flash.NumSpeculativeTokens != 5 {
		t.Error("building an entry mutated the shared recipe config")
	}
}

func TestDSparkSpecDecoEntryDeepSeekV4Flash0731(t *testing.T) {
	entry, ok := speculativeDecodingByPreset["deepseek-ai/deepseek-v4-flash-0731"]
	if !ok {
		t.Fatal("missing dspark entry for deepseek-ai/deepseek-v4-flash-0731")
	}
	if entry.UserFacing != "deepseek-ai/DeepSeek-V4-Flash-0731" {
		t.Errorf("UserFacing = %q", entry.UserFacing)
	}
	cfg := entry.Config
	if cfg == nil || cfg.Method != SpeculativeDecodingMethodDSpark || cfg.DSpark == nil {
		t.Fatalf("want dspark config, got %+v", cfg)
	}
	// https://recipes.vllm.ai/deepseek-ai/DeepSeek-V4-Flash
	want := model.DSparkConfig{NumSpeculativeTokens: 7, DraftSampleMethod: "probabilistic"}
	if *cfg.DSpark != want {
		t.Errorf("DSpark = %+v, want %+v", *cfg.DSpark, want)
	}
	if len(cfg.EngineArgs) != 0 {
		t.Errorf("EngineArgs = %v, want none", cfg.EngineArgs)
	}
}

func TestCPUOffloadedWeightSize(t *testing.T) {
	for name, want := range map[string]string{
		"deepseek-v4.1-flash-1017": "183.1Gi",
		"deepseek-v4-flash-0731":   "",
	} {
		got, _ := longestPrefixMatch(name, cpuOffloadedWeightSizePrefixMap)
		if got != want {
			t.Errorf("%s: CPU-offloaded weight size = %q, want %q", name, got, want)
		}
	}
}
