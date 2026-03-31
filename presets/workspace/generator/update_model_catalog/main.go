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

// Command update_model_catalog updates model_catalog.yaml with latest
// values from HuggingFace.
//
// Usage:
//
//	go run ./presets/workspace/generator/update_model_catalog                       # Update all existing entries
//	go run ./presets/workspace/generator/update_model_catalog --repos org/m1,org/m2 # Add or update specific repos
//	go run ./presets/workspace/generator/update_model_catalog --dry-run             # Show changes without writing
//	go run ./presets/workspace/generator/update_model_catalog --token <HF_TOKEN>    # Use auth token
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaito-project/kaito/presets/workspace/generator"
)

func main() {
	repos := flag.String("repos", "", "Comma-separated list of repos to add or update (default: all existing)")
	dryRun := flag.Bool("dry-run", false, "Show changes without writing to file")
	token := flag.String("token", os.Getenv("HF_TOKEN"), "HuggingFace API token (default: $HF_TOKEN)")
	catalog := flag.String("catalog", filepath.Join("presets", "workspace", "models", "model_catalog.yaml"), "Path to model_catalog.yaml")
	flag.Parse()

	var repoList []string
	if *repos != "" {
		for _, r := range strings.Split(*repos, ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				repoList = append(repoList, r)
			}
		}
	}

	if err := updateModelCatalog(repoList, *dryRun, *token, *catalog); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func formatDiff(old, new *generator.CatalogEntry) string {
	if old == nil {
		lines := []string{fmt.Sprintf("  + ADD %s", new.Name)}
		lines = append(lines, catalogFieldLines(new)...)
		return strings.Join(lines, "\n")
	}

	oldFields := catalogFields(old)
	newFields := catalogFields(new)

	keySet := make(map[string]struct{})
	for k := range oldFields {
		keySet[k] = struct{}{}
	}
	for k := range newFields {
		keySet[k] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var changes []string
	for _, k := range keys {
		ov := oldFields[k]
		nv := newFields[k]
		if ov != nv {
			changes = append(changes, fmt.Sprintf("      %s: %v -> %v", k, ov, nv))
		}
	}

	if len(changes) == 0 {
		return fmt.Sprintf("  = %s (no changes)", new.Name)
	}
	return fmt.Sprintf("  ~ UPDATE %s\n%s", new.Name, strings.Join(changes, "\n"))
}

func catalogFields(e *generator.CatalogEntry) map[string]string {
	m := map[string]string{
		"modelFileSize":     e.ModelFileSize,
		"architectures":     fmt.Sprintf("%v", e.Architectures),
		"modelTokenLimit":   fmt.Sprintf("%d", e.ModelTokenLimit),
		"hiddenSize":        fmt.Sprintf("%d", e.HiddenSize),
		"numHiddenLayers":   fmt.Sprintf("%d", e.NumHiddenLayers),
		"numAttentionHeads": fmt.Sprintf("%d", e.NumAttentionHeads),
		"numKeyValueHeads":  fmt.Sprintf("%d", e.NumKeyValueHeads),
	}
	if e.Description != "" {
		m["description"] = e.Description
	}
	if e.License != "" {
		m["license"] = e.License
	}
	if e.PipelineTag != "" {
		m["pipelineTag"] = e.PipelineTag
	}
	if len(e.BaseModel) > 0 {
		m["baseModel"] = fmt.Sprintf("%v", e.BaseModel)
	}
	if e.HeadDim > 0 {
		m["headDim"] = fmt.Sprintf("%d", e.HeadDim)
	}
	if e.KVLoraRank > 0 {
		m["kvLoraRank"] = fmt.Sprintf("%d", e.KVLoraRank)
	}
	if e.QKRopeHeadDim > 0 {
		m["qkRopeHeadDim"] = fmt.Sprintf("%d", e.QKRopeHeadDim)
	}
	if e.LoadFormat != "" {
		m["loadFormat"] = e.LoadFormat
	}
	if e.ConfigFormat != "" {
		m["configFormat"] = e.ConfigFormat
	}
	if e.TokenizerMode != "" {
		m["tokenizerMode"] = e.TokenizerMode
	}
	return m
}

func catalogFieldLines(e *generator.CatalogEntry) []string {
	var lines []string
	fields := catalogFields(e)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("      %s: %s", k, fields[k]))
	}
	return lines
}

func updateModelCatalog(repos []string, dryRun bool, token, catalogPath string) error {
	existing, err := generator.LoadCatalog(catalogPath)
	if err != nil {
		return fmt.Errorf("failed to load catalog: %v", err)
	}

	existingByRepo := make(map[string]int)
	for i, e := range existing {
		existingByRepo[strings.ToLower(e.Name)] = i
	}

	targets := repos
	if len(targets) == 0 {
		for _, e := range existing {
			targets = append(targets, e.Name)
		}
	}
	if len(targets) == 0 {
		fmt.Println("No repos to process. Use --repos to specify, or populate the catalog.")
		return nil
	}

	updated := make([]generator.CatalogEntry, len(existing))
	copy(updated, existing)
	hasChanges := false

	for _, repo := range targets {
		fmt.Printf("Fetching %s...\n", repo)
		newEntry, err := generator.FetchCatalogEntry(repo, token)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ERROR: %v\n", err)
			continue
		}

		idx, exists := existingByRepo[strings.ToLower(repo)]
		if !exists {
			diff := formatDiff(nil, newEntry)
			fmt.Println(diff)
			updated = append(updated, *newEntry)
			existingByRepo[strings.ToLower(repo)] = len(updated) - 1
			hasChanges = true
		} else {
			oldEntry := &updated[idx]
			diff := formatDiff(oldEntry, newEntry)
			fmt.Println(diff)
			if !strings.Contains(diff, "no changes") {
				updated[idx] = *newEntry
				hasChanges = true
			}
		}
	}

	if !hasChanges {
		fmt.Println("\nNo changes detected.")
		return nil
	}

	if dryRun {
		fmt.Println("\n--dry-run: no file written.")
	} else {
		if err := generator.SaveCatalog(catalogPath, updated); err != nil {
			return fmt.Errorf("failed to save catalog: %v", err)
		}
		fmt.Printf("\nWritten to %s\n", catalogPath)
	}

	return nil
}
