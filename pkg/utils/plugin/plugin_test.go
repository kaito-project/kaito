package plugin

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	// Define test cases
	testCases := []struct {
		name         string
		yamlContent  string
		expectError  bool
		expectedInfo map[string]*ModelInfo
	}{
		{
			name: "Success",
			yamlContent: `
models:
  - name: model-a
    type: text-generation
    version: https://huggingface.co/tiiuae/falcon-7b/commit/ec89142b67d748a1865ea4451372db8313ada0d8
    runtime: tfs
    tag: 0.1.0
  - name: model-b
    type: llama2-completion
    runtime: tfs
    tag: 0.0.4
    downloadAtRuntime: true
`,
			expectError: false,
			expectedInfo: map[string]*ModelInfo{
				"model-a": {
					Name:              "model-a",
					ModelType:         "text-generation",
					Version:           "https://huggingface.co/tiiuae/falcon-7b/commit/ec89142b67d748a1865ea4451372db8313ada0d8",
					Runtime:           "tfs",
					Tag:               "0.1.0",
					DownloadAtRuntime: false,
				},
				"model-b": {
					Name:              "model-b",
					ModelType:         "llama2-completion",
					Version:           "",
					Runtime:           "tfs",
					Tag:               "0.0.4",
					DownloadAtRuntime: true,
				},
			},
		},
		{
			name:        "Empty Models List",
			yamlContent: "models: []",
			expectError: false,
			// Expect an initialized but empty map
			expectedInfo: nil,
		},
		{
			name:        "No Models Key",
			yamlContent: "some_other_key: value",
			expectError: false,
			// Expect an initialized but empty map if 'models' key is missing but YAML is valid
			expectedInfo: nil,
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup: Create a temporary file or simulate non-existence based on yamlContent
			var filePath string
			tempFile, err := os.CreateTemp("", "supported-models-*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			filePath = tempFile.Name()

			defer os.Remove(filePath)

			if _, err := tempFile.WriteString(tc.yamlContent); err != nil {
				tempFile.Close() // Close file before failing
				t.Fatalf("Failed to write to temp file: %v", err)
			}

			if err := tempFile.Close(); err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}

			reg := &ModelRegister{}
			err = reg.InitModelInfo(filePath)

			if tc.expectError {
				assert.Error(t, err)
				assert.Nil(t, reg.info, "reg.info should be nil on error")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedInfo, reg.info)
			}
		})
	}
}
