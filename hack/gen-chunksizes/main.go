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

//go:build ignore

// Command gen-chunksizes regenerates pkg/cache/dacs/chunksize/chunksizes_generated.go
// from pkg/cache/dacs/chunksize/models.yaml.
//
// For each model it reads the safetensors header of every shard the same way
// the run:ai streamer does — the leading little-endian uint64 header length
// followed by the JSON header, whose per-tensor `data_offsets` [begin,end] give
// each tensor's byte size — then recommends the modal per-tensor byte size (the
// size the most tensors share) as RUNAI_STREAMER_CHUNK_BYTESIZE, capping any
// tensor larger than 32 MiB at 32 MiB before counting and flooring the final
// recommendation at 2 MiB. A per-tensor size table is emitted above each
// recommendation for reference.
//
// Usage:
//
//	go run ./hack/gen-chunksizes/main.go \
//	  -models pkg/cache/dacs/chunksize/models.yaml \
//	  -out    pkg/cache/dacs/chunksize/chunksizes_generated.go
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

type modelEntry struct {
	Name     string `json:"name"`
	Repo     string `json:"repo"`
	Revision string `json:"revision"`
}

type modelList struct {
	Models []modelEntry `json:"models"`
}

// cacheEntry is the on-disk cache of a model's fetched tensor stats, keyed by
// repo@revision so re-runs of the generator skip the (slow) HuggingFace fetch.
type cacheEntry struct {
	Stats        []cacheKind `json:"stats"`
	TotalTensors int         `json:"totalTensors"`
	TotalBytes   int64       `json:"totalBytes"`
}

type cacheKind struct {
	Kind  string  `json:"kind"`
	Count int     `json:"count"`
	Each  int64   `json:"each"`
	Dtype string  `json:"dtype"`
	Shape []int64 `json:"shape"`
}

type safetensorsIndex struct {
	WeightMap map[string]string `json:"weight_map"`
	Metadata  struct {
		TotalSize int64 `json:"total_size"`
	} `json:"metadata"`
}

type tensorMeta struct {
	Dtype       string   `json:"dtype"`
	Shape       []int64  `json:"shape"`
	DataOffsets [2]int64 `json:"data_offsets"`
}

// kindStat aggregates all tensors that share the same logical kind (layer and
// expert indices normalized away).
type kindStat struct {
	kind  string
	count int
	each  int64 // per-tensor byte size (tensors of a kind share a size)
	dtype string
	shape []int64
}

var (
	layerIdxRe  = regexp.MustCompile(`\.\d+\.`)
	expertIdxRe = regexp.MustCompile(`experts\.\d+`)
)

// maxChunkByteSize caps the recommendation. run:ai reads at most one chunk per
// request, so a tensor larger than the chunk is fetched in several requests; a
// tensor smaller than the chunk still costs a full chunk read. There is no
// benefit to a chunk larger than 32 MiB, so any tensor bigger than this is
// counted as 32 MiB when choosing the modal size.
const maxChunkByteSize int64 = 32 * 1024 * 1024

// minChunkByteSize floors the recommendation. Some layouts (e.g. MXFP4 models
// that stack all experts of a layer into a single large tensor) have their byte
// mass in a few huge tensors while the most numerous tensors are tiny norms and
// biases, so a pure modal size would pick a degenerately small chunk. Never
// recommend a chunk smaller than 2 MiB.
const minChunkByteSize int64 = 2 * 1024 * 1024

// clampSize caps a per-tensor byte size at maxChunkByteSize for the purpose of
// modal-size selection.
func clampSize(n int64) int64 {
	if n > maxChunkByteSize {
		return maxChunkByteSize
	}
	return n
}

// applyFloor raises a chunk size to minChunkByteSize when it is below the floor.
func applyFloor(n int64) int64 {
	if n < minChunkByteSize {
		return minChunkByteSize
	}
	return n
}

func normalizeKind(name string) string {
	n := layerIdxRe.ReplaceAllString(name, ".N.")
	n = expertIdxRe.ReplaceAllString(n, "experts.E")
	return n
}

func main() {
	modelsPath := flag.String("models", "pkg/cache/dacs/chunksize/models.yaml", "path to the models list YAML")
	outPath := flag.String("out", "pkg/cache/dacs/chunksize/chunksizes_generated.go", "path to the generated Go file")
	cacheDir := flag.String("cache", filepath.Join(os.TempDir(), "kaito-chunksizes-cache"), "dir to cache fetched tensor stats (per repo@revision); set empty to disable")
	flag.Parse()

	raw, err := os.ReadFile(*modelsPath)
	if err != nil {
		log.Fatalf("read models list %s: %v", *modelsPath, err)
	}
	var list modelList
	if err := yaml.Unmarshal(raw, &list); err != nil {
		log.Fatalf("parse models list: %v", err)
	}
	if len(list.Models) == 0 {
		log.Fatalf("no models in %s", *modelsPath)
	}

	client := &http.Client{Timeout: 120 * time.Second}

	var b strings.Builder
	writeHeader(&b, *modelsPath)
	seen := map[string]string{} // map key -> model name that first produced it
	for _, m := range list.Models {
		if m.Name == "" || m.Repo == "" {
			log.Fatalf("model entry missing name or repo: %+v", m)
		}
		rev := m.Revision
		if rev == "" {
			rev = "main"
		}
		log.Printf("processing %s (%s @ %s)", m.Name, m.Repo, rev)
		stats, totalTensors, totalBytes, err := readModelCached(client, *cacheDir, m.Repo, rev)
		if err != nil {
			log.Fatalf("read %s: %v", m.Name, err)
		}
		modal, modalCount := modalSize(stats)
		recommended := applyFloor(modal)

		// The runtime model name (PresetParam.Name) is the lowercased final path
		// segment of the HuggingFace repo (see presets generator), so key the map
		// by that. Emit the short preset name as an alias when it differs.
		key := repoBasenameKey(m.Repo)
		alias := strings.ToLower(m.Name)
		if prev, ok := seen[key]; ok {
			log.Fatalf("duplicate key %q from models %q and %q", key, prev, m.Name)
		}
		seen[key] = m.Name
		if alias != key {
			if prev, ok := seen[alias]; ok {
				log.Fatalf("duplicate alias key %q from models %q and %q", alias, prev, m.Name)
			}
			seen[alias] = m.Name
		}
		writeModelBlock(&b, m, rev, key, alias, stats, totalTensors, totalBytes, modal, recommended, modalCount)
	}
	b.WriteString("}\n")

	src, err := format.Source([]byte(b.String()))
	if err != nil {
		log.Fatalf("gofmt generated source: %v\n----\n%s", err, b.String())
	}
	if err := os.WriteFile(*outPath, src, 0o644); err != nil {
		log.Fatalf("write %s: %v", *outPath, err)
	}
	log.Printf("wrote %s (%d models)", *outPath, len(list.Models))
}

// repoBasenameKey returns the lowercased final path segment of a HuggingFace
// repo id — the value KAITO uses as PresetParam.Name at runtime and thus the map
// lookup key (e.g. "mistralai/Mistral-7B-v0.3" -> "mistral-7b-v0.3").
func repoBasenameKey(repo string) string {
	b := repo
	if i := strings.LastIndex(b, "/"); i >= 0 {
		b = b[i+1:]
	}
	return strings.ToLower(b)
}

// readModelCached wraps readModel with an on-disk cache keyed by repo@revision.
func readModelCached(client *http.Client, cacheDir, repo, rev string) ([]kindStat, int, int64, error) {
	var fp string
	if cacheDir != "" {
		safe := strings.NewReplacer("/", "_", ":", "_").Replace(repo + "@" + rev)
		fp = filepath.Join(cacheDir, safe+".json")
		if data, rerr := os.ReadFile(fp); rerr == nil {
			var ce cacheEntry
			if json.Unmarshal(data, &ce) == nil && len(ce.Stats) > 0 {
				stats := make([]kindStat, len(ce.Stats))
				for i, s := range ce.Stats {
					stats[i] = kindStat{kind: s.Kind, count: s.Count, each: s.Each, dtype: s.Dtype, shape: s.Shape}
				}
				log.Printf("  cache hit: %s", fp)
				return stats, ce.TotalTensors, ce.TotalBytes, nil
			}
		}
	}
	stats, totalTensors, totalBytes, err := readModel(client, repo, rev)
	if err != nil {
		return nil, 0, 0, err
	}
	if fp != "" {
		ce := cacheEntry{TotalTensors: totalTensors, TotalBytes: totalBytes}
		for _, s := range stats {
			ce.Stats = append(ce.Stats, cacheKind{Kind: s.kind, Count: s.count, Each: s.each, Dtype: s.dtype, Shape: s.shape})
		}
		if data, merr := json.MarshalIndent(ce, "", "  "); merr == nil {
			if mkerr := os.MkdirAll(cacheDir, 0o755); mkerr == nil {
				_ = os.WriteFile(fp, data, 0o644)
			}
		}
	}
	return stats, totalTensors, totalBytes, nil
}

// readModel fetches the safetensors headers for all shards of repo@revision and
// returns per-kind stats (sorted by total bytes desc), the total tensor count,
// and the total tensor byte size.
func readModel(client *http.Client, repo, revision string) (stats []kindStat, totalTensors int, totalBytes int64, err error) {
	base := fmt.Sprintf("https://huggingface.co/%s/resolve/%s/", repo, revision)

	shards, err := listShards(client, base)
	if err != nil {
		return nil, 0, 0, err
	}

	// Aggregate by kind. Tensors of a kind share a size, so we track count and
	// a representative size/dtype/shape.
	agg := map[string]*kindStat{}
	for _, shard := range shards {
		hdr, herr := readHeader(client, base+shard)
		if herr != nil {
			return nil, 0, 0, fmt.Errorf("shard %s: %w", shard, herr)
		}
		for name, meta := range hdr {
			size := meta.DataOffsets[1] - meta.DataOffsets[0]
			totalTensors++
			totalBytes += size
			k := normalizeKind(name)
			s := agg[k]
			if s == nil {
				s = &kindStat{kind: k, each: size, dtype: meta.Dtype, shape: meta.Shape}
				agg[k] = s
			}
			s.count++
		}
	}

	stats = make([]kindStat, 0, len(agg))
	for _, s := range agg {
		stats = append(stats, *s)
	}
	// Sort by total bytes desc, then kind for determinism.
	sort.Slice(stats, func(i, j int) bool {
		ti := stats[i].each * int64(stats[i].count)
		tj := stats[j].each * int64(stats[j].count)
		if ti != tj {
			return ti > tj
		}
		return stats[i].kind < stats[j].kind
	})
	return stats, totalTensors, totalBytes, nil
}

// listShards returns the set of .safetensors shard filenames for the model,
// using model.safetensors.index.json when present and falling back to a single
// model.safetensors file.
func listShards(client *http.Client, base string) ([]string, error) {
	body, status, err := httpGet(client, base+"model.safetensors.index.json", nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusOK {
		var idx safetensorsIndex
		if err := json.Unmarshal(body, &idx); err != nil {
			return nil, fmt.Errorf("parse index.json: %w", err)
		}
		set := map[string]struct{}{}
		for _, shard := range idx.WeightMap {
			set[shard] = struct{}{}
		}
		shards := make([]string, 0, len(set))
		for s := range set {
			shards = append(shards, s)
		}
		sort.Strings(shards)
		return shards, nil
	}
	// Single-file model.
	return []string{"model.safetensors"}, nil
}

// readHeader reads the safetensors header of a shard and returns its per-tensor
// metadata (excluding the __metadata__ entry).
func readHeader(client *http.Client, url string) (map[string]tensorMeta, error) {
	// The header length is a little-endian uint64 in the first 8 bytes.
	lenBytes, status, err := httpGet(client, url, rangeHeader(0, 7))
	if err != nil {
		return nil, err
	}
	if status != http.StatusPartialContent && status != http.StatusOK {
		return nil, fmt.Errorf("header length GET status %d", status)
	}
	if len(lenBytes) < 8 {
		return nil, fmt.Errorf("short header length read: %d bytes", len(lenBytes))
	}
	n := binary.LittleEndian.Uint64(lenBytes[:8])

	hdrBytes, status, err := httpGet(client, url, rangeHeader(8, 8+int64(n)-1))
	if err != nil {
		return nil, err
	}
	if status != http.StatusPartialContent && status != http.StatusOK {
		return nil, fmt.Errorf("header GET status %d", status)
	}

	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(hdrBytes, &rawMap); err != nil {
		return nil, fmt.Errorf("parse header json: %w", err)
	}
	out := make(map[string]tensorMeta, len(rawMap))
	for name, raw := range rawMap {
		if name == "__metadata__" {
			continue
		}
		var m tensorMeta
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse tensor %q: %w", name, err)
		}
		out[name] = m
	}
	return out, nil
}

func rangeHeader(start, end int64) map[string]string {
	return map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", start, end)}
}

// httpGet performs a GET with optional headers and up to 3 attempts, returning
// the body and status code. A 404 is returned without error so callers can fall
// back (e.g. missing index.json).
func httpGet(client *http.Client, url string, headers map[string]string) ([]byte, int, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			return body, resp.StatusCode, nil
		}
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			continue
		}
		return body, resp.StatusCode, nil
	}
	return nil, 0, fmt.Errorf("GET %s failed after retries: %w", url, lastErr)
}

// modalSize returns the per-tensor byte size shared by the most tensors, with a
// tie broken toward the larger size, and the count of tensors at that size.
// Each tensor's size is capped at maxChunkByteSize before counting, so tensors
// larger than 32 MiB contribute to the 32 MiB bucket.
func modalSize(stats []kindStat) (size int64, count int) {
	bySize := map[int64]int{}
	for _, s := range stats {
		bySize[clampSize(s.each)] += s.count
	}
	for sz, c := range bySize {
		if c > count || (c == count && sz > size) {
			size, count = sz, c
		}
	}
	return size, count
}

func writeHeader(b *strings.Builder, modelsPath string) {
	fmt.Fprint(b, `// Copyright (c) KAITO authors.
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

// Code generated by hack/gen-chunksizes; DO NOT EDIT.
`)
	fmt.Fprintf(b, "// Source: %s\n", modelsPath)
	fmt.Fprint(b, `// Regenerate with: make generate-chunksizes
//
// Each recommendation is the modal per-tensor byte size from the model's
// safetensors headers (the size the most tensors share, with any tensor larger
// than 32 MiB counted as 32 MiB and the result floored at 2 MiB); see the table
// above each entry.

package chunksize

// recommendedChunkSizes maps a KAITO runtime model name to the recommended
// run:ai streamer chunk byte size (RUNAI_STREAMER_CHUNK_BYTESIZE). Keys are the
// lowercased final path segment of the HuggingFace repo — the value KAITO sets
// as PresetParam.Name at runtime (e.g. "mistralai/Mistral-7B-v0.3" ->
// "mistral-7b-v0.3"); the short preset name is added as an alias when it
// differs. Look up via Recommended, which normalizes its argument.
var recommendedChunkSizes = map[string]int{
`)
}

func writeModelBlock(b *strings.Builder, m modelEntry, rev, key, alias string, stats []kindStat, totalTensors int, totalBytes int64, modal, recommended int64, modalCount int) {
	fmt.Fprintf(b, "\t// === %s ===\n", m.Name)
	fmt.Fprintf(b, "\t// repo:    %s @ %s\n", m.Repo, rev)
	fmt.Fprintf(b, "\t// tensors: %s   total: %s B (%s)\n", commas(int64(totalTensors)), commas(totalBytes), humanGiB(totalBytes))
	fmt.Fprint(b, "\t//\n")
	fmt.Fprintf(b, "\t//   %8s  %16s  %10s  %-6s %-16s %s\n", "count", "size(bytes)", "size", "dtype", "shape", "tensor")
	for _, s := range stats {
		marker := ""
		if clampSize(s.each) == modal {
			if s.each > modal {
				marker = "  <= modal (counted as 32 MiB cap)"
			} else {
				marker = "  <= modal (most tensors at this size)"
			}
		}
		fmt.Fprintf(b, "\t//   %8d  %16s  %10s  %-6s %-16s %s%s\n",
			s.count, commas(s.each), humanMiB(s.each), s.dtype, shapeStr(s.shape), s.kind, marker)
	}
	fmt.Fprint(b, "\t//\n")
	fmt.Fprintf(b, "\t// modal size: %s B (%s) shared by the most tensors (%s tensors at\n",
		commas(modal), humanMiB(modal), commas(int64(modalCount)))
	fmt.Fprint(b, "\t// this size, tensors larger than 32 MiB counted as 32 MiB).\n")
	if recommended != modal {
		fmt.Fprintf(b, "\t// recommended: %s B (%s) — raised to the 2 MiB minimum chunk floor.\n",
			commas(recommended), humanMiB(recommended))
	} else {
		fmt.Fprintf(b, "\t// recommended: %s B (%s).\n", commas(recommended), humanMiB(recommended))
	}
	fmt.Fprint(b, "\t// The full tensor layout above is retained verbatim; no rows are filtered out.\n")
	// Key by the runtime model name (lowercased repo basename); add the short
	// preset name as an alias when it differs so both forms resolve.
	fmt.Fprintf(b, "\t%q: %d,\n", key, recommended)
	if alias != key {
		fmt.Fprintf(b, "\t%q: %d, // alias (preset name)\n", alias, recommended)
	}
	fmt.Fprint(b, "\n")
}

func shapeStr(shape []int64) string {
	parts := make([]string, len(shape))
	for i, d := range shape {
		parts[i] = fmt.Sprintf("%d", d)
	}
	if len(parts) == 1 {
		return "(" + parts[0] + ",)"
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func humanMiB(n int64) string {
	return fmt.Sprintf("%.3f MiB", float64(n)/(1024*1024))
}

func humanGiB(n int64) string {
	return fmt.Sprintf("%.2f GiB", float64(n)/(1024*1024*1024))
}

// commas formats n with thousands separators.
func commas(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
