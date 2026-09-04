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
	"testing"

	"github.com/kaito-project/kaito/pkg/model"
)

func TestResolveSpeculativeDecodingMethod(t *testing.T) {
	tests := []struct {
		name       string
		presetRepo string
		wantMethod string
		wantPP     bool
	}{
		{"registered mtp preset (R1)", "deepseek-ai/deepseek-r1-0528", "mtp", true},
		{"registered mtp preset (V3)", "deepseek-ai/DeepSeek-V3-0324", "mtp", true},
		{"registered mtp preset (V3.2)", "deepseek-ai/DeepSeek-V3.2", "mtp", true},
		{"registered mtp preset (GLM-5.2-FP8)", "zai-org/GLM-5.2-FP8", "mtp", true},
		{"registered non-catalog mtp preset (DeepSeek-V4-Flash preview)", "deepseek-ai/DeepSeek-V4-Flash", "mtp", true},
		{"registered non-catalog mtp preset (DeepSeek-V4-Flash NVFP4)", "nvidia/DeepSeek-V4-Flash-NVFP4", "mtp", true},
		{"registered non-catalog mtp preset (MiMo-7B-Base)", "XiaomiMiMo/MiMo-7B-Base", "mtp", true},
		{"unregistered preset falls back to ngram", "meta-llama/Llama-3.1-8B-Instruct", "ngram", true},
		{"empty preset falls back to ngram", "", "ngram", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveSpeculativeDecodingMethod(tc.presetRepo)
			if got != tc.wantMethod {
				t.Errorf("ResolveSpeculativeDecodingMethod(%q) = %q, want %q", tc.presetRepo, got, tc.wantMethod)
			}
			if SpeculativeDecodingMethodSupportsPipelineParallelism(got) != tc.wantPP {
				t.Errorf("PP support for %q = %v, want %v", got, !tc.wantPP, tc.wantPP)
			}
		})
	}
}

func TestResolveSpeculativeDecodingMethodForPresetName(t *testing.T) {
	tests := []struct {
		name       string
		presetName string
		wantMethod string
	}{
		{"legacy short alias resolves to mtp", "deepseek-r1-0528", "mtp"},
		{"second legacy short alias resolves to mtp", "deepseek-v3-0324", "mtp"},
		{"canonical repo name still resolves", "deepseek-ai/deepseek-v3.2", "mtp"},
		{"in-catalog canonical repo with mtp resolves", "zai-org/GLM-5.2-FP8", "mtp"},
		{"non-catalog DeepSeek V4 preview exact repo resolves", "deepseek-ai/DeepSeek-V4-Flash", "mtp"},
		{"non-catalog DeepSeek V4 NVFP4 exact repo resolves", "nvidia/DeepSeek-V4-Flash-NVFP4", "mtp"},
		{"non-catalog MiMo exact repo resolves", "XiaomiMiMo/MiMo-7B-Base", "mtp"},
		{"non-tuned preset falls back to ngram", "llama-3.1-8b-instruct", "ngram"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveSpeculativeDecodingMethodForPresetName(tc.presetName)
			if got != tc.wantMethod {
				t.Errorf("ResolveSpeculativeDecodingMethodForPresetName(%q) = %q, want %q", tc.presetName, got, tc.wantMethod)
			}
		})
	}
}

func TestSpeculativeDecodingMethodSupportsPipelineParallelism(t *testing.T) {
	cases := map[string]bool{
		"ngram":   true,
		"mtp":     true,
		"eagle":   false,
		"eagle3":  false,
		"dspark":  false, // conservative default for unknown/experimental
		"unknown": false,
	}
	for method, want := range cases {
		if got := SpeculativeDecodingMethodSupportsPipelineParallelism(method); got != want {
			t.Errorf("method=%q supportsPP = %v, want %v", method, got, want)
		}
	}
}

func TestSupportedSpeculativeDecodingPresets(t *testing.T) {
	presets := SupportedSpeculativeDecodingPresets()
	want := map[string]bool{
		"deepseek-r1-0528":               true,
		"deepseek-v3-0324":               true,
		"deepseek-v3.2":                  true,
		"zai-org/GLM-5.2-FP8":            true,
		"deepseek-ai/DeepSeek-V4-Flash":  true,
		"nvidia/DeepSeek-V4-Flash-NVFP4": true,
		"XiaomiMiMo/MiMo-7B-Base":        true,
	}

	if len(presets) != len(want) {
		t.Fatalf("expected %d supported presets, got %d: %v", len(want), len(presets), presets)
	}
	for _, preset := range presets {
		if !want[preset] {
			t.Fatalf("unexpected preset in supported list: %q (full=%v)", preset, presets)
		}
		delete(want, preset)
	}
	if len(want) != 0 {
		t.Fatalf("missing supported presets: %v", want)
	}
}

func TestSpeculativeDecodingByPresetEntries(t *testing.T) {
	tests := []struct {
		repoKey   string
		wantUser  string
		wantDepth int
	}{
		{"deepseek-ai/deepseek-r1-0528", "deepseek-r1-0528", 1},
		{"deepseek-ai/deepseek-v3-0324", "deepseek-v3-0324", 1},
		{"deepseek-ai/deepseek-v3.2", "deepseek-v3.2", 1},
		{"zai-org/glm-5.2-fp8", "zai-org/GLM-5.2-FP8", 5},
		{"deepseek-ai/deepseek-v4-flash", "deepseek-ai/DeepSeek-V4-Flash", 3},
		{"nvidia/deepseek-v4-flash-nvfp4", "nvidia/DeepSeek-V4-Flash-NVFP4", 3},
		{"xiaomimimo/mimo-7b-base", "XiaomiMiMo/MiMo-7B-Base", 1},
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
		if entry.Config.MTP.NumSpeculativeTokens != tc.wantDepth {
			t.Errorf("entry %q: NumSpeculativeTokens = %d, want %d", tc.repoKey, entry.Config.MTP.NumSpeculativeTokens, tc.wantDepth)
		}
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
			if cfg.DSpark != nil {
				t.Errorf("%s: method=mtp but DSpark is non-nil", key)
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
			MTP:    &model.MTPConfig{NumSpeculativeTokens: 1},
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

	// Mutate copy, original should be unaffected
	c.SpeculativeDecoding.MTP.NumSpeculativeTokens = 5
	if p.SpeculativeDecoding.MTP.NumSpeculativeTokens != 1 {
		t.Fatal("DeepCopy: mutation leaked to original")
	}
}
