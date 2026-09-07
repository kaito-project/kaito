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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applySpeculativeDecoding(tc.ws, tc.runtime, tc.preset, tc.userOverride)
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
