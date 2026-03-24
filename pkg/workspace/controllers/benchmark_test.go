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

package controllers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBenchmarkResult(t *testing.T) {
	tests := map[string]struct {
		logs         string
		expectErr    bool
		expectTPM    string
		expectConfig map[string]string
	}{
		"single result line": {
			logs:      "some startup log\nKAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {\"vllm_total_tpm\":12345.67,\"ttft_avg_ms\":100,\"tpot_avg_ms\":50}\n",
			expectTPM: "12345.67",
		},
		"takes last of multiple result lines": {
			// First line has -1 (failed probe), second is the successful result.
			logs: "KAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {\"vllm_total_tpm\":-1,\"ttft_avg_ms\":-1,\"tpot_avg_ms\":-1}\n" +
				"KAITO_BENCHMARK_RESULT 2026-01-01T00:00:01Z {\"vllm_total_tpm\":99999,\"ttft_avg_ms\":10,\"tpot_avg_ms\":5}\n",
			expectTPM: "99999",
		},
		"result embedded in noisy log lines": {
			logs:      "2026/01/01 vllm startup\n[info] model loaded\nKAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {\"vllm_total_tpm\":500,\"ttft_avg_ms\":200,\"tpot_avg_ms\":80}\n[info] ready\n",
			expectTPM: "500",
		},
		"tag present but no space after timestamp": {
			logs:      "KAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z\n",
			expectErr: true,
		},
		"tag not present": {
			logs:      "no benchmark here\nsome other log\n",
			expectErr: true,
		},
		"malformed json payload": {
			logs:      "KAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {not-valid-json}\n",
			expectErr: true,
		},
		"integer tpm value": {
			logs:      "KAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {\"vllm_total_tpm\":1000000,\"ttft_avg_ms\":50,\"tpot_avg_ms\":10}\n",
			expectTPM: "1000000",
		},
		"zero tpm treated as failure": {
			logs:      "KAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {\"vllm_total_tpm\":0,\"ttft_avg_ms\":0,\"tpot_avg_ms\":0}\n",
			expectErr: true,
		},
		"negative tpm treated as failure": {
			logs:      "KAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {\"vllm_total_tpm\":-1,\"ttft_avg_ms\":-1,\"tpot_avg_ms\":-1}\n",
			expectErr: true,
		},
		"config parsed from KAITO_BENCHMARK_CONFIG line": {
			logs: "KAITO_BENCHMARK_CONFIG 2026-01-01T00:00:00Z {\"duration_sec\":60,\"input_tokens\":2048,\"output_tokens\":256,\"max_concurrency\":523}\n" +
				"KAITO_BENCHMARK_RESULT 2026-01-01T00:00:02Z {\"vllm_total_tpm\":12345.67,\"ttft_avg_ms\":100,\"tpot_avg_ms\":50}\n",
			expectTPM: "12345.67",
			expectConfig: map[string]string{
				"durationSec":    "60",
				"inputTokens":    "2048",
				"outputTokens":   "256",
				"maxConcurrency": "523",
			},
		},
		"config absent when KAITO_BENCHMARK_CONFIG not logged": {
			logs:         "KAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {\"vllm_total_tpm\":500,\"ttft_avg_ms\":0,\"tpot_avg_ms\":0}\n",
			expectTPM:    "500",
			expectConfig: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := parseBenchmarkResult(strings.NewReader(tc.logs))
			if tc.expectErr {
				assert.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)

			m, ok := result.Metrics[BenchmarkMetricPeakTPM]
			require.True(t, ok, "expected %q key in Metrics map", BenchmarkMetricPeakTPM)
			assert.Equal(t, tc.expectTPM, m.Value)
			assert.Equal(t, BenchmarkDesc, m.Description)
			assert.Equal(t, BenchmarkMetricUnit, m.Unit)
			assert.Equal(t, tc.expectConfig, m.Config)
		})
	}
}
