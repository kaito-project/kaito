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

package mig

import (
	"fmt"
	"regexp"
	"strconv"
)

const (
	// MIGResourcePrefix is the Kubernetes extended resource prefix for MIG devices.
	MIGResourcePrefix = "nvidia.com/mig-"
)

// migProfileRegex matches valid MIG profile strings like "1g.5gb", "3g.40gb", "7g.80gb".
var migProfileRegex = regexp.MustCompile(`^(\d+)g\.(\d+)gb$`)

// knownProfiles is the set of valid MIG profiles across supported GPU models.
var knownProfiles = map[string]bool{
	// A30 (24GB)
	"1g.6gb":  true,
	"2g.12gb": true,
	"4g.24gb": true,
	// A100 (40GB)
	"1g.5gb":  true,
	"2g.10gb": true,
	"3g.20gb": true,
	"4g.20gb": true,
	"7g.40gb": true,
	// A100 (80GB) / H100 (80GB)
	"1g.10gb": true,
	"2g.20gb": true,
	"3g.40gb": true,
	"4g.40gb": true,
	"7g.80gb": true,
}

// ParseMIGProfile parses a MIG profile string (e.g., "1g.10gb") and returns
// the number of compute slices and the memory in GB.
func ParseMIGProfile(profile string) (computeSlices int, memoryGB int, err error) {
	matches := migProfileRegex.FindStringSubmatch(profile)
	if matches == nil {
		return 0, 0, fmt.Errorf("invalid MIG profile format %q: expected format like '1g.10gb'", profile)
	}
	computeSlices, _ = strconv.Atoi(matches[1])
	memoryGB, _ = strconv.Atoi(matches[2])
	if computeSlices == 0 {
		return 0, 0, fmt.Errorf("invalid MIG profile %q: compute slices must be > 0", profile)
	}
	if memoryGB == 0 {
		return 0, 0, fmt.Errorf("invalid MIG profile %q: memory must be > 0", profile)
	}
	return computeSlices, memoryGB, nil
}

// MIGResourceName returns the Kubernetes extended resource name for a MIG profile.
// For example, "1g.10gb" returns "nvidia.com/mig-1g.10gb".
func MIGResourceName(profile string) string {
	return MIGResourcePrefix + profile
}

// ValidateMIGProfile checks if a MIG profile string is syntactically valid
// and corresponds to a known NVIDIA MIG profile.
func ValidateMIGProfile(profile string) error {
	_, _, err := ParseMIGProfile(profile)
	if err != nil {
		return err
	}
	if !knownProfiles[profile] {
		return fmt.Errorf("unknown MIG profile %q: must be one of %v", profile, KnownMIGProfiles())
	}
	return nil
}

// KnownMIGProfiles returns a sorted list of all known valid MIG profile strings.
func KnownMIGProfiles() []string {
	profiles := make([]string, 0, len(knownProfiles))
	for p := range knownProfiles {
		profiles = append(profiles, p)
	}
	return profiles
}
