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

package progress

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validPayload = `# HELP model_mirror_download_speed_bytes_per_second Average download speed since the download started
# TYPE model_mirror_download_speed_bytes_per_second gauge
model_mirror_download_speed_bytes_per_second 4.1892e+08
# HELP model_mirror_download_remaining_seconds Estimated seconds to completion; -1 when unknown
# TYPE model_mirror_download_remaining_seconds gauge
model_mirror_download_remaining_seconds 128
`

func TestParse(t *testing.T) {
	got, err := Parse(strings.NewReader(validPayload))
	require.NoError(t, err)
	assert.Equal(t, int64(418920000), got.SpeedBytesPerSecond)
	assert.Equal(t, int64(128), got.RemainingSeconds)
}

func TestParseSelectsByNameNotPosition(t *testing.T) {
	// Metrics may be added later; the parser must not depend on line order.
	reordered := `# TYPE model_mirror_download_remaining_seconds gauge
model_mirror_download_remaining_seconds 42
# TYPE some_other_metric gauge
some_other_metric 999
# TYPE model_mirror_download_speed_bytes_per_second gauge
model_mirror_download_speed_bytes_per_second 100
`
	got, err := Parse(strings.NewReader(reordered))
	require.NoError(t, err)
	assert.Equal(t, int64(100), got.SpeedBytesPerSecond)
	assert.Equal(t, int64(42), got.RemainingSeconds)
}

func TestParseMissingMetricIsError(t *testing.T) {
	partial := `# TYPE model_mirror_download_speed_bytes_per_second gauge
model_mirror_download_speed_bytes_per_second 100
`
	_, err := Parse(strings.NewReader(partial))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model_mirror_download_remaining_seconds")
}

func TestParseMalformedIsError(t *testing.T) {
	_, err := Parse(strings.NewReader("this is not prometheus text {{{\n"))
	require.Error(t, err)
}

func TestParseTruncatesTowardZero(t *testing.T) {
	// A sub-1 B/s speed reports 0 rather than rounding up.
	payload := `# TYPE model_mirror_download_speed_bytes_per_second gauge
model_mirror_download_speed_bytes_per_second 0.9
# TYPE model_mirror_download_remaining_seconds gauge
model_mirror_download_remaining_seconds -1
`
	got, err := Parse(strings.NewReader(payload))
	require.NoError(t, err)
	assert.Equal(t, int64(0), got.SpeedBytesPerSecond)
	assert.Equal(t, int64(-1), got.RemainingSeconds)
}
