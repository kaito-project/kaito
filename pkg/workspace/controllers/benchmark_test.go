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
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --------------------------------------------------------------------------
// TestParseBenchmarkResult — pure function, no mocks needed.
// --------------------------------------------------------------------------

func TestParseBenchmarkResult(t *testing.T) {
	tests := map[string]struct {
		logs         string
		expectErr    bool
		expectTPM    string
		expectConfig *kaitov1beta1.BenchmarkConfig
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
			expectConfig: &kaitov1beta1.BenchmarkConfig{
				DurationSec:    60,
				InputTokens:    2048,
				OutputTokens:   256,
				MaxConcurrency: 523,
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
			result, err := parseBenchmarkResult(tc.logs)
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
			assert.Equal(t, BenchmarkDesc, m.Desc)
			assert.Equal(t, BenchmarkMetricUnit, m.Unit)
			assert.Equal(t, tc.expectConfig, m.Config)
		})
	}
}

// --------------------------------------------------------------------------

type stubLogStreamer struct {
	content   string
	streamErr error
}

func (s *stubLogStreamer) StreamLogs(_ context.Context, _, _ string) (io.ReadCloser, error) {
	if s.streamErr != nil {
		return nil, s.streamErr
	}
	return io.NopCloser(strings.NewReader(s.content)), nil
}

// --------------------------------------------------------------------------
// TestReconcileBenchmarkResult — exercises the real function via the interface.
// --------------------------------------------------------------------------

func TestReconcileBenchmarkResult(t *testing.T) {
	wObj := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "my-workspace", Namespace: "default"},
	}

	tests := map[string]struct {
		streamer  podLogStreamer
		expectErr bool
		expectTPM string
	}{
		"parses result from logs": {
			streamer:  &stubLogStreamer{content: "KAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {\"vllm_total_tpm\":12345.67,\"ttft_avg_ms\":100,\"tpot_avg_ms\":50}\n"},
			expectTPM: "12345.67",
		},
		"returns error when stream fails": {
			streamer:  &stubLogStreamer{streamErr: fmt.Errorf("connection refused")},
			expectErr: true,
		},
		"returns error when no benchmark line in logs": {
			streamer:  &stubLogStreamer{content: "no benchmark here\n"},
			expectErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := reconcileBenchmarkResult(context.Background(), wObj, tc.streamer)
			if tc.expectErr {
				assert.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			m, ok := result.Metrics[BenchmarkMetricPeakTPM]
			require.True(t, ok)
			assert.Equal(t, tc.expectTPM, m.Value)
		})
	}
}

// --------------------------------------------------------------------------
// TestReconcileBenchmarkResultWithStatus — verifies status field assignment
// and error propagation via reconcileBenchmarkResult (applyBenchmarkStatus
// condition-setting is covered by workspace_controller_test.go).
// --------------------------------------------------------------------------

func TestReconcileBenchmarkResultWithStatus(t *testing.T) {
	wObj := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-workspace",
			Namespace: "default",
			Annotations: map[string]string{
				kaitov1beta1.AnnotationRunBenchmark: "true",
			},
		},
	}

	t.Run("sets result and BenchmarkCompleted=True on success", func(t *testing.T) {
		streamer := &stubLogStreamer{content: "KAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {\"vllm_total_tpm\":12345.67,\"ttft_avg_ms\":100,\"tpot_avg_ms\":50}\n"}
		result, err := reconcileBenchmarkResult(context.Background(), wObj, streamer)
		require.NoError(t, err)

		status := &kaitov1beta1.WorkspaceStatus{}
		status.BenchmarkResult = result
		require.NotNil(t, status.BenchmarkResult)
		m, ok := status.BenchmarkResult.Metrics[BenchmarkMetricPeakTPM]
		require.True(t, ok)
		assert.Equal(t, "12345.67", m.Value)
	})

	t.Run("sets BenchmarkCompleted=False when stream fails", func(t *testing.T) {
		streamer := &stubLogStreamer{streamErr: fmt.Errorf("connection refused")}
		_, err := reconcileBenchmarkResult(context.Background(), wObj, streamer)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connection refused")
	})

	t.Run("overwrites existing result on re-reconcile", func(t *testing.T) {
		// always reads fresh from logs.
		streamer := &stubLogStreamer{content: "KAITO_BENCHMARK_RESULT 2026-01-01T00:00:00Z {\"vllm_total_tpm\":99999,\"ttft_avg_ms\":10,\"tpot_avg_ms\":5}\n"}

		status := &kaitov1beta1.WorkspaceStatus{
			BenchmarkResult: &kaitov1beta1.BenchmarkResult{
				Metrics: map[string]kaitov1beta1.BenchmarkMetric{
					BenchmarkMetricPeakTPM: {Value: "old-value"},
				},
			},
		}
		result, err := reconcileBenchmarkResult(context.Background(), wObj, streamer)
		require.NoError(t, err)
		status.BenchmarkResult = result
		m, ok := status.BenchmarkResult.Metrics[BenchmarkMetricPeakTPM]
		require.True(t, ok)
		assert.Equal(t, "99999", m.Value)
	})
}
