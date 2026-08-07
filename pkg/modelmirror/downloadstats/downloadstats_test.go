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

package downloadstats

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

const realJobLogTail = "" +
	"/usr/local/lib/python3.11/site-packages/huggingface_hub/constants.py:294: FutureWarning: The `HF_HUB_ENABLE_HF_TRANSFER` environment variable is deprecated\n" +
	"  warnings.warn(\n" +
	"\x1b[90mHint: A new version of huggingface_hub (1.26.0) is available!\x1b[0m\n" +
	"Fetching 20 files:   0%|          | 0/20 [00:00<?, ?it/s]" +
	"\rFetching 20 files:  40%|████      | 8/20 [00:01<00:01,  6.89it/s]" +
	"\rFetching 20 files: 100%|██████████| 20/20 [00:19<00:00,  1.01it/s]\n" +
	"KAITO_DOWNLOAD_STATS seconds=21 bytes=7644768499\n" +
	"\x1b[32m✓ Downloaded\x1b[0m\n"

func TestParse(t *testing.T) {
	cases := []struct {
		name      string
		logs      string
		wantStats *Stats
		wantErr   bool
	}{
		{
			name:      "valid marker",
			logs:      "KAITO_DOWNLOAD_STATS seconds=21 bytes=7644768499\n",
			wantStats: &Stats{DurationSeconds: 21, Bytes: 7644768499},
		},
		{
			name:      "real job log with tqdm and ansi noise",
			logs:      realJobLogTail,
			wantStats: &Stats{DurationSeconds: 21, Bytes: 7644768499},
		},
		{
			name:      "no marker returns nil without error",
			logs:      "Fetching 20 files: 100%\nDownloaded\n",
			wantStats: nil,
		},
		{
			name:      "empty log returns nil without error",
			logs:      "",
			wantStats: nil,
		},
		{
			name: "multiple markers - last wins",
			logs: "KAITO_DOWNLOAD_STATS seconds=99 bytes=1\n" +
				"KAITO_DOWNLOAD_STATS seconds=21 bytes=7644768499\n",
			wantStats: &Stats{DurationSeconds: 21, Bytes: 7644768499},
		},
		{
			name:    "malformed seconds is an error",
			logs:    "KAITO_DOWNLOAD_STATS seconds=abc bytes=100\n",
			wantErr: true,
		},
		{
			name:    "malformed bytes is an error",
			logs:    "KAITO_DOWNLOAD_STATS seconds=10 bytes=xyz\n",
			wantErr: true,
		},
		{
			name:    "missing bytes field is an error",
			logs:    "KAITO_DOWNLOAD_STATS seconds=10\n",
			wantErr: true,
		},
		{
			name:      "zero duration is parsed, not rejected",
			logs:      "KAITO_DOWNLOAD_STATS seconds=0 bytes=500\n",
			wantStats: &Stats{DurationSeconds: 0, Bytes: 500},
		},
		{
			name: "huge tqdm carriage-return blob does not mask the marker",
			logs: strings.Repeat("Fetching 20 files:  50%|xxxxxxxx| 10/20 [00:01<00:01,  6.89it/s]\r", 20000) +
				"\nKAITO_DOWNLOAD_STATS seconds=21 bytes=7644768499\n",
			wantStats: &Stats{DurationSeconds: 21, Bytes: 7644768499},
		},
		{
			name:    "field with embedded equals in value is an error",
			logs:    "KAITO_DOWNLOAD_STATS seconds=1=2 bytes=100\n",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tc.logs))
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantStats, got)
		})
	}
}

func TestStatsThroughputMBps(t *testing.T) {
	cases := []struct {
		name  string
		stats Stats
		want  string
	}{
		{
			name:  "typical download",
			stats: Stats{DurationSeconds: 21, Bytes: 7644768499},
			want:  "364.04",
		},
		{
			name:  "zero duration returns empty",
			stats: Stats{DurationSeconds: 0, Bytes: 500},
			want:  "",
		},
		{
			name:  "zero bytes",
			stats: Stats{DurationSeconds: 10, Bytes: 0},
			want:  "0.00",
		},
		{
			name:  "negative duration returns empty",
			stats: Stats{DurationSeconds: -1, Bytes: 500},
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.stats.ThroughputMBps()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFetch(t *testing.T) {
	cs := kubefake.NewClientset()

	got, err := Fetch(context.Background(), cs, "default", "mirror-download-abc")
	require.NoError(t, err)
	assert.Nil(t, got)
}
