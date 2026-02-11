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
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// MIGResourcePrefix is the Kubernetes extended resource prefix for MIG devices.
	MIGResourcePrefix = "nvidia.com/mig-"
)

// migProfileRegex matches valid MIG profile strings like "1g.5gb", "3g.40gb", "7g.80gb",
// and decimal memory values like "1g.16.5gb". Also supports +me media extension suffix.
var migProfileRegex = regexp.MustCompile(`^(\d+)g\.([\d.]+)gb(\+me)?$`)

// knownProfiles is the set of valid MIG profiles across supported GPU models.
// Uses the "mixed strategy" naming convention (nvidia.com/mig-<profile>).
// With "single strategy", all MIG devices appear as nvidia.com/gpu instead.
// KAITO uses mixed strategy because users need to specify which partition size they want.
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
	"1g.20gb": true,
	"2g.20gb": true,
	"3g.40gb": true,
	"4g.40gb": true,
	"7g.80gb": true,
	// H200 (141GB HBM3e)
	"1g.16.5gb": true,
	"2g.33gb":   true,
	"4g.66gb":   true,
	"7g.141gb":  true,
	// B200 / Blackwell (180-192GB HBM3e)
	"1g.23gb":  true,
	"2g.46gb":  true,
	"4g.92gb":  true,
	"7g.180gb": true,
}

// knownMEProfiles is the set of profiles that support the +me (media extension) suffix.
var knownMEProfiles = map[string]bool{
	"1g.5gb":    true,
	"1g.6gb":    true,
	"1g.10gb":   true,
	"1g.20gb":   true,
	"1g.16.5gb": true,
	"1g.23gb":   true,
	"2g.12gb":   true,
}

// ParseMIGProfile parses a MIG profile string (e.g., "1g.10gb", "1g.16.5gb+me") and returns
// the number of compute slices and the memory in GB (floored to int for fractional values).
func ParseMIGProfile(profile string) (computeSlices int, memoryGB int, err error) {
	// Strip +me suffix for parsing
	cleanProfile := strings.TrimSuffix(profile, "+me")
	matches := migProfileRegex.FindStringSubmatch(cleanProfile)
	if matches == nil {
		return 0, 0, fmt.Errorf("invalid MIG profile format %q: expected format like '1g.10gb' or '1g.16.5gb'", profile)
	}
	computeSlices, _ = strconv.Atoi(matches[1])
	memFloat, parseErr := strconv.ParseFloat(matches[2], 64)
	if parseErr != nil {
		return 0, 0, fmt.Errorf("invalid MIG profile %q: cannot parse memory value", profile)
	}
	memoryGB = int(math.Floor(memFloat))
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
	// Check +me suffix separately
	if strings.HasSuffix(profile, "+me") {
		baseProfile := strings.TrimSuffix(profile, "+me")
		if !knownMEProfiles[baseProfile] {
			return fmt.Errorf("unknown MIG profile %q: media extension (+me) is not supported for this profile", profile)
		}
		if !knownProfiles[baseProfile] {
			return fmt.Errorf("unknown MIG profile %q: must be one of %v", profile, KnownMIGProfiles())
		}
		return nil
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
	sort.Strings(profiles)
	return profiles
}
