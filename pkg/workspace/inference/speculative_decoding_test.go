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
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestVllmFormat(t *testing.T) {
	tests := []struct {
		name    string
		sd      *pkgmodel.SpeculativeDecodingConfig
		wantErr bool
		check   func(t *testing.T, jsonStr string)
	}{
		{
			name:    "nil config",
			sd:      nil,
			wantErr: true,
		},
		{
			name: "mtp",
			sd: &pkgmodel.SpeculativeDecodingConfig{
				Method: "mtp",
				MTP:    &pkgmodel.MTPConfig{NumSpeculativeTokens: 1},
			},
			check: func(t *testing.T, jsonStr string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if m["method"] != "mtp" {
					t.Errorf("method = %v, want mtp", m["method"])
				}
				if m["num_speculative_tokens"] != float64(1) {
					t.Errorf("num_speculative_tokens = %v, want 1", m["num_speculative_tokens"])
				}
			},
		},
		{
			name: "mtp with draft model",
			sd: &pkgmodel.SpeculativeDecodingConfig{
				Method: "mtp",
				MTP: &pkgmodel.MTPConfig{
					NumSpeculativeTokens: 1,
					Model:                "example/draft-model",
				},
			},
			check: func(t *testing.T, jsonStr string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if m["method"] != "mtp" {
					t.Errorf("method = %v, want mtp", m["method"])
				}
				if m["num_speculative_tokens"] != float64(1) {
					t.Errorf("num_speculative_tokens = %v, want 1", m["num_speculative_tokens"])
				}
				if m["model"] != "example/draft-model" {
					t.Errorf("model = %v, want example/draft-model", m["model"])
				}
			},
		},
		{
			name: "mtp nil sub-config",
			sd: &pkgmodel.SpeculativeDecodingConfig{
				Method: "mtp",
			},
			wantErr: true,
		},
		{
			name: "ngram",
			sd: &pkgmodel.SpeculativeDecodingConfig{
				Method: "ngram",
				NGram:  &pkgmodel.NGramConfig{NumSpeculativeTokens: 5, PromptLookupMax: 4},
			},
			check: func(t *testing.T, jsonStr string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				if m["method"] != "ngram" {
					t.Errorf("method = %v, want ngram", m["method"])
				}
				if m["num_speculative_tokens"] != float64(5) {
					t.Errorf("num_speculative_tokens = %v, want 5", m["num_speculative_tokens"])
				}
				if m["prompt_lookup_max"] != float64(4) {
					t.Errorf("prompt_lookup_max = %v, want 4", m["prompt_lookup_max"])
				}
			},
		},
		{
			name: "dspark",
			sd: &pkgmodel.SpeculativeDecodingConfig{
				Method: "dspark",
				DSpark: &pkgmodel.DSparkConfig{
					NumSpeculativeTokens:       5,
					DraftSampleMethod:          "probabilistic",
					RejectionSampleMethod:      "block",
					EnableAdaptiveVerification: true,
				},
				EngineArgs: map[string]string{"max-num-seqs": "256"},
			},
			check: func(t *testing.T, jsonStr string) {
				var m map[string]any
				if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				want := map[string]any{
					"method":                       "dspark",
					"num_speculative_tokens":       float64(5),
					"draft_sample_method":          "probabilistic",
					"rejection_sample_method":      "block",
					"enable_adaptive_verification": true,
				}
				if len(m) != len(want) {
					t.Errorf("got keys %v, want exactly %v (engine args must not leak into speculative-config)", m, want)
				}
				for k, v := range want {
					if m[k] != v {
						t.Errorf("%s = %v, want %v", k, m[k], v)
					}
				}
			},
		},
		{
			// DeepSeek-V4-Flash recipe: https://recipes.vllm.ai/deepseek-ai/DeepSeek-V4-Flash
			name: "dspark renders only the fields a recipe sets",
			sd: &pkgmodel.SpeculativeDecodingConfig{
				Method: "dspark",
				DSpark: &pkgmodel.DSparkConfig{
					NumSpeculativeTokens: 7,
					DraftSampleMethod:    "probabilistic",
				},
			},
			check: func(t *testing.T, jsonStr string) {
				want := `{"draft_sample_method":"probabilistic","method":"dspark","num_speculative_tokens":7}`
				if jsonStr != want {
					t.Errorf("got %s, want %s", jsonStr, want)
				}
			},
		},
		{
			name: "dspark nil sub-config",
			sd: &pkgmodel.SpeculativeDecodingConfig{
				Method: "dspark",
			},
			wantErr: true,
		},
		{
			name: "unknown method",
			sd: &pkgmodel.SpeculativeDecodingConfig{
				Method: "eagle",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := vllmFormat(tc.sd)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestShellSingleQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`hello`, `'hello'`},
		{`it's`, `'it'\''s'`},
		{`{"method":"mtp"}`, `'{"method":"mtp"}'`},
	}
	for _, tc := range tests {
		got := shellSingleQuote(tc.input)
		if got != tc.want {
			t.Errorf("shellSingleQuote(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestLoadUserSpeculativeConfig(t *testing.T) {
	newWS := func(configName string) *v1beta1.Workspace {
		return &v1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "ws", Namespace: "default"},
			Inference:  &v1beta1.InferenceSpec{Config: configName},
		}
	}

	t.Run("no config returns false", func(t *testing.T) {
		got, err := loadUserSpeculativeConfig(context.Background(), nil, newWS(""))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Fatal("expected no override")
		}
	})

	t.Run("config with speculative-config returns true", func(t *testing.T) {
		mockClient := test.NewClient()
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "default"},
			Data: map[string]string{
				pkgmodel.ConfigfileNameVLLM: "vllm:\n  speculative-config: '{\"method\":\"ngram\"}'\n",
			},
		}
		mockClient.CreateOrUpdateObjectInMap(cm)
		mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)

		got, err := loadUserSpeculativeConfig(context.Background(), mockClient, newWS("cfg"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Fatal("expected override to be detected")
		}
	})

	t.Run("config without speculative-config returns false", func(t *testing.T) {
		mockClient := test.NewClient()
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "default"},
			Data: map[string]string{
				pkgmodel.ConfigfileNameVLLM: "vllm:\n  max-model-len: \"4096\"\n",
			},
		}
		mockClient.CreateOrUpdateObjectInMap(cm)
		mockClient.On("Get", mock.Anything, mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)

		got, err := loadUserSpeculativeConfig(context.Background(), mockClient, newWS("cfg"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Fatal("expected no override")
		}
	})
}

func TestApplySpeculativeDecoding(t *testing.T) {
	presetWithSD := func() *pkgmodel.PresetParam {
		return &pkgmodel.PresetParam{
			RuntimeParam: pkgmodel.RuntimeParam{
				VLLM: pkgmodel.VLLMParam{
					ModelRunParams: map[string]string{},
				},
			},
			SpeculativeDecoding: &pkgmodel.SpeculativeDecodingConfig{
				Method: "mtp",
				MTP:    &pkgmodel.MTPConfig{NumSpeculativeTokens: 1},
			},
		}
	}
	presetWithDraftMTP := func() *pkgmodel.PresetParam {
		return &pkgmodel.PresetParam{
			RuntimeParam: pkgmodel.RuntimeParam{
				VLLM: pkgmodel.VLLMParam{
					ModelRunParams: map[string]string{},
				},
			},
			SpeculativeDecoding: &pkgmodel.SpeculativeDecodingConfig{
				Method: "mtp",
				MTP: &pkgmodel.MTPConfig{
					NumSpeculativeTokens: 1,
					Model:                "example/draft-model",
				},
			},
		}
	}
	presetNoSD := func() *pkgmodel.PresetParam {
		return &pkgmodel.PresetParam{
			RuntimeParam: pkgmodel.RuntimeParam{
				VLLM: pkgmodel.VLLMParam{ModelRunParams: map[string]string{}},
			},
		}
	}
	newWS := func(annVal string, targetNodes int32) *v1beta1.Workspace {
		ws := &v1beta1.Workspace{}
		if annVal != "" {
			ws.Annotations = map[string]string{v1beta1.AnnotationEnableSpeculativeDecoding: annVal}
		}
		ws.Status.TargetNodeCount = targetNodes
		return ws
	}

	tests := []struct {
		name         string
		ws           *v1beta1.Workspace
		runtime      pkgmodel.RuntimeName
		preset       *pkgmodel.PresetParam
		userOverride bool
		wantDecision SpecDecoDecision
		wantInjected bool
		wantContains string // required substring in the injected --speculative-config blob; "" defaults to method=mtp
	}{
		{
			name:         "annotation absent -> skip, no flag",
			ws:           newWS("", 1),
			runtime:      pkgmodel.RuntimeNameVLLM,
			preset:       presetWithSD(),
			wantDecision: SpecDecoSkip,
			wantInjected: false,
		},
		{
			name:         "annotation false -> skip, no flag",
			ws:           newWS("false", 1),
			runtime:      pkgmodel.RuntimeNameVLLM,
			preset:       presetWithSD(),
			wantDecision: SpecDecoSkip,
			wantInjected: false,
		},
		{
			name:         "annotation true + non-vllm runtime -> skip",
			ws:           newWS("true", 1),
			runtime:      pkgmodel.RuntimeNameHuggingfaceTransformers,
			preset:       presetWithSD(),
			wantDecision: SpecDecoSkip,
			wantInjected: false,
		},
		{
			name:         "annotation true + preset without SD -> ngram fallback",
			ws:           newWS("true", 1),
			runtime:      pkgmodel.RuntimeNameVLLM,
			preset:       presetNoSD(),
			wantDecision: SpecDecoInjectedNGramFallback,
			wantInjected: true,
			wantContains: `"method":"ngram"`,
		},
		{
			name:         "annotation true + user config override -> skip injection",
			ws:           newWS("true", 1),
			runtime:      pkgmodel.RuntimeNameVLLM,
			preset:       presetWithSD(),
			userOverride: true,
			wantDecision: SpecDecoConfigMapOverride,
			wantInjected: false,
			wantContains: "",
		},
		{
			name:         "annotation true + multi-node + mtp -> injected (PP-compatible)",
			ws:           newWS("true", 2),
			runtime:      pkgmodel.RuntimeNameVLLM,
			preset:       presetWithSD(),
			wantDecision: SpecDecoInjected,
			wantInjected: true,
		},
		{
			name:         "annotation true + multi-node + ngram fallback -> injected (PP-compatible)",
			ws:           newWS("true", 2),
			runtime:      pkgmodel.RuntimeNameVLLM,
			preset:       presetNoSD(),
			wantDecision: SpecDecoInjectedNGramFallback,
			wantInjected: true,
			wantContains: `"method":"ngram"`,
		},
		{
			name:         "annotation true + supported preset + single-node -> injected",
			ws:           newWS("true", 1),
			runtime:      pkgmodel.RuntimeNameVLLM,
			preset:       presetWithSD(),
			wantDecision: SpecDecoInjected,
			wantInjected: true,
		},
		{
			name:         "annotation true + mtp draft model -> injected",
			ws:           newWS("true", 1),
			runtime:      pkgmodel.RuntimeNameVLLM,
			preset:       presetWithDraftMTP(),
			wantDecision: SpecDecoInjected,
			wantInjected: true,
			wantContains: `"model":"example/draft-model"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applySpeculativeDecoding(tc.ws, tc.runtime, tc.preset, tc.userOverride, tc.ws.Status.TargetNodeCount > 1, nil, 1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantDecision {
				t.Errorf("decision = %v, want %v", got, tc.wantDecision)
			}
			flag, injected := tc.preset.VLLM.ModelRunParams["speculative-config"]
			if injected != tc.wantInjected {
				t.Errorf("speculative-config injected = %v, want %v (flag=%q)", injected, tc.wantInjected, flag)
			}
			if tc.wantInjected {
				want := tc.wantContains
				if want == "" {
					want = `"method":"mtp"`
				}
				if !strings.Contains(flag, want) {
					t.Errorf("speculative-config value %q missing %q", flag, want)
				}
				if !strings.HasPrefix(flag, "'") || !strings.HasSuffix(flag, "'") {
					t.Errorf("speculative-config value %q not shell-single-quoted", flag)
				}
			}
		})
	}
}

func TestApplySpeculativeDecodingDSpark(t *testing.T) {
	newPreset := func() (*pkgmodel.PresetParam, *pkgmodel.SpeculativeDecodingConfig) {
		shared := &pkgmodel.SpeculativeDecodingConfig{
			Method: "dspark",
			DSpark: &pkgmodel.DSparkConfig{
				NumSpeculativeTokens:       5,
				DraftSampleMethod:          "probabilistic",
				RejectionSampleMethod:      "block",
				EnableAdaptiveVerification: true,
			},
			EngineArgs: map[string]string{
				"max-num-batched-tokens": "4096",
				"max-num-seqs":           "256",
			},
		}
		return &pkgmodel.PresetParam{
			RuntimeParam: pkgmodel.RuntimeParam{
				VLLM: pkgmodel.VLLMParam{ModelRunParams: map[string]string{"max-num-batched-tokens": "8192"}},
			},
			SpeculativeDecoding: shared,
		}, shared
	}
	ws := &v1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{v1beta1.AnnotationEnableSpeculativeDecoding: "true"},
	}}

	tests := []struct {
		name             string
		pipelineParallel bool
		wantAdaptive     bool
	}{
		{name: "single node keeps adaptive verification", wantAdaptive: true},
		{name: "pipeline parallel turns adaptive verification off", pipelineParallel: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preset, shared := newPreset()
			got, err := applySpeculativeDecoding(ws, pkgmodel.RuntimeNameVLLM, preset, false, tc.pipelineParallel, nil, 1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != SpecDecoInjected {
				t.Errorf("decision = %v, want %v", got, SpecDecoInjected)
			}
			params := preset.VLLM.ModelRunParams
			flag := params["speculative-config"]
			for _, want := range []string{`"method":"dspark"`, `"num_speculative_tokens":5`} {
				if !strings.Contains(flag, want) {
					t.Errorf("speculative-config %q missing %q", flag, want)
				}
			}
			if got := strings.Contains(flag, `"enable_adaptive_verification":true`); got != tc.wantAdaptive {
				t.Errorf("adaptive verification enabled = %v, want %v in %q", got, tc.wantAdaptive, flag)
			}
			if params["max-num-batched-tokens"] != "4096" {
				t.Errorf("max-num-batched-tokens = %q, want engine arg 4096 to override the preset default", params["max-num-batched-tokens"])
			}
			if params["max-num-seqs"] != "256" {
				t.Errorf("max-num-seqs = %q, want 256", params["max-num-seqs"])
			}
			if !shared.DSpark.EnableAdaptiveVerification {
				t.Error("shared preset config was mutated")
			}
		})
	}

	t.Run("user speculative-config skips engine args", func(t *testing.T) {
		preset, _ := newPreset()
		got, err := applySpeculativeDecoding(ws, pkgmodel.RuntimeNameVLLM, preset, true, false, nil, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != SpecDecoConfigMapOverride {
			t.Errorf("decision = %v, want %v", got, SpecDecoConfigMapOverride)
		}
		params := preset.VLLM.ModelRunParams
		if _, ok := params["speculative-config"]; ok {
			t.Error("speculative-config injected despite user override")
		}
		if params["max-num-batched-tokens"] != "8192" || params["max-num-seqs"] != "" {
			t.Errorf("engine args applied despite user override: %v", params)
		}
	})
}

func TestApplySpeculativeDecodingDSparkGPUMemoryGate(t *testing.T) {
	nc80adis := &sku.GPUConfig{GPUCount: 2, GPUMem: resource.MustParse("188Gi"), GPUModel: "NVIDIA H100"}
	nd96isr := &sku.GPUConfig{GPUCount: 8, GPUMem: resource.MustParse("640Gi"), GPUModel: "NVIDIA H100"}
	dspark := &pkgmodel.SpeculativeDecodingConfig{
		Method:     "dspark",
		DSpark:     &pkgmodel.DSparkConfig{NumSpeculativeTokens: 7},
		EngineArgs: map[string]string{"max-num-seqs": "128"},
	}
	mtp := &pkgmodel.SpeculativeDecodingConfig{Method: "mtp", MTP: &pkgmodel.MTPConfig{NumSpeculativeTokens: 1}}
	preset := func(sd *pkgmodel.SpeculativeDecodingConfig, total, offloaded string) *pkgmodel.PresetParam {
		return &pkgmodel.PresetParam{
			Metadata:                pkgmodel.Metadata{CPUOffloadedWeightSize: offloaded},
			TotalSafeTensorFileSize: total,
			RuntimeParam:            pkgmodel.RuntimeParam{VLLM: pkgmodel.VLLMParam{ModelRunParams: map[string]string{}}},
			SpeculativeDecoding:     sd,
		}
	}
	ws := &v1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{v1beta1.AnnotationEnableSpeculativeDecoding: "true"},
	}}

	tests := []struct {
		name      string
		preset    *pkgmodel.PresetParam
		gpuConfig *sku.GPUConfig
		numNodes  int
		want      SpecDecoDecision
	}{
		// About 0.95 GiB per GPU after weights.
		{"dspark on a tight node is skipped", preset(dspark, "155.43Gi", ""), nc80adis, 1, SpecDecoSkippedLowGPUMemory},
		// About 50 GiB per GPU.
		{"dspark with room is injected", preset(dspark, "155.43Gi", ""), nd96isr, 1, SpecDecoInjected},
		// About 32 GiB per GPU with the Engram tables in CPU memory; 7.7 GiB without.
		{"cpu-offloaded weights count toward headroom", preset(dspark, "475.26Gi", "183.1Gi"), nd96isr, 1, SpecDecoInjected},
		{"without the offload the same model is skipped", preset(dspark, "475.26Gi", ""), nd96isr, 1, SpecDecoSkippedLowGPUMemory},
		// About 42.6 GiB per GPU across two nodes.
		{"weights sharded across nodes", preset(dspark, "155.43Gi", ""), nc80adis, 2, SpecDecoInjected},
		{"unknown gpu memory keeps the opt-in", preset(dspark, "155.43Gi", ""), nil, 1, SpecDecoInjected},
		{"unknown model size keeps the opt-in", preset(dspark, "", ""), nc80adis, 1, SpecDecoInjected},
		{"mtp is not gated", preset(mtp, "155.43Gi", ""), nc80adis, 1, SpecDecoInjected},
		{"ngram fallback is not gated", preset(nil, "155.43Gi", ""), nc80adis, 1, SpecDecoInjectedNGramFallback},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applySpeculativeDecoding(ws, pkgmodel.RuntimeNameVLLM, tc.preset, false, tc.numNodes > 1, tc.gpuConfig, tc.numNodes)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("decision = %v, want %v", got, tc.want)
			}
			params := tc.preset.VLLM.ModelRunParams
			_, injected := params["speculative-config"]
			if injected != (tc.want != SpecDecoSkippedLowGPUMemory) {
				t.Errorf("speculative-config injected = %v, params %v", injected, params)
			}
			if tc.want == SpecDecoSkippedLowGPUMemory && params["max-num-seqs"] != "" {
				t.Errorf("engine args applied while skipped: %v", params)
			}
		})
	}

	t.Run("user speculative-config bypasses the gate", func(t *testing.T) {
		p := preset(dspark, "155.43Gi", "")
		got, err := applySpeculativeDecoding(ws, pkgmodel.RuntimeNameVLLM, p, true, false, nc80adis, 1)
		if err != nil || got != SpecDecoConfigMapOverride {
			t.Fatalf("decision = %v, err = %v; want %v", got, err, SpecDecoConfigMapOverride)
		}
	})
}
