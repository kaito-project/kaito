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
	"testing"
)

func TestParseMIGProfile(t *testing.T) {
	tests := []struct {
		name       string
		profile    string
		wantSlices int
		wantMemory int
		wantErr    bool
	}{
		{"valid 1g.5gb", "1g.5gb", 1, 5, false},
		{"valid 1g.10gb", "1g.10gb", 1, 10, false},
		{"valid 2g.20gb", "2g.20gb", 2, 20, false},
		{"valid 3g.40gb", "3g.40gb", 3, 40, false},
		{"valid 4g.40gb", "4g.40gb", 4, 40, false},
		{"valid 7g.80gb", "7g.80gb", 7, 80, false},
		{"valid 1g.6gb", "1g.6gb", 1, 6, false},
		{"valid 2g.12gb", "2g.12gb", 2, 12, false},
		// H100 (94GB)
		{"valid 1g.12gb (H100-94GB)", "1g.12gb", 1, 12, false},
		{"valid 7g.94gb (H100-94GB)", "7g.94gb", 7, 94, false},
		// H200 (141GB)
		{"valid 1g.18gb (H200)", "1g.18gb", 1, 18, false},
		{"valid 2g.35gb (H200)", "2g.35gb", 2, 35, false},
		{"valid 3g.71gb (H200)", "3g.71gb", 3, 71, false},
		{"valid 7g.141gb (H200)", "7g.141gb", 7, 141, false},
		// B200 / Blackwell
		{"valid 1g.23gb (B200)", "1g.23gb", 1, 23, false},
		{"valid 2g.45gb (B200)", "2g.45gb", 2, 45, false},
		{"valid 3g.90gb (B200)", "3g.90gb", 3, 90, false},
		{"valid 7g.180gb (B200)", "7g.180gb", 7, 180, false},
		// Decimal memory parsing (hypothetical future profile)
		{"decimal memory floors correctly", "1g.16.5gb", 1, 16, false},
		// +me media extension rejected (not a distinct K8s resource)
		{"reject +me suffix", "1g.10gb+me", 0, 0, true},
		// Invalid cases
		{"empty string", "", 0, 0, true},
		{"invalid format", "invalid", 0, 0, true},
		{"missing memory", "1g", 0, 0, true},
		{"missing slices", "10gb", 0, 0, true},
		{"zero slices", "0g.10gb", 0, 0, true},
		{"zero memory", "1g.0gb", 0, 0, true},
		{"wrong unit", "1g.10tb", 0, 0, true},
		{"spaces", " 1g.10gb ", 0, 0, true},
		{"mixed case", "1G.10GB", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slices, memory, err := ParseMIGProfile(tt.profile)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMIGProfile(%q) error = %v, wantErr %v", tt.profile, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if slices != tt.wantSlices {
					t.Errorf("ParseMIGProfile(%q) slices = %d, want %d", tt.profile, slices, tt.wantSlices)
				}
				if memory != tt.wantMemory {
					t.Errorf("ParseMIGProfile(%q) memory = %d, want %d", tt.profile, memory, tt.wantMemory)
				}
			}
		})
	}
}

func TestMIGResourceName(t *testing.T) {
	tests := []struct {
		profile string
		want    string
	}{
		{"1g.10gb", "nvidia.com/mig-1g.10gb"},
		{"3g.40gb", "nvidia.com/mig-3g.40gb"},
		{"7g.80gb", "nvidia.com/mig-7g.80gb"},
	}

	for _, tt := range tests {
		t.Run(tt.profile, func(t *testing.T) {
			got := MIGResourceName(tt.profile)
			if got != tt.want {
				t.Errorf("MIGResourceName(%q) = %q, want %q", tt.profile, got, tt.want)
			}
		})
	}
}

func TestValidateMIGProfile(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		wantErr bool
	}{
		{"valid known profile 1g.5gb", "1g.5gb", false},
		{"valid known profile 1g.10gb", "1g.10gb", false},
		{"valid known profile 7g.80gb", "7g.80gb", false},
		{"valid known profile 4g.24gb", "4g.24gb", false},
		{"valid known H200 profile 1g.18gb", "1g.18gb", false},
		{"valid known H100-94GB profile 1g.12gb", "1g.12gb", false},
		{"valid known B200 profile 1g.23gb", "1g.23gb", false},
		{"reject +me profile", "1g.10gb+me", true},
		{"valid format but unknown profile", "5g.50gb", true},
		{"invalid format", "bad", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMIGProfile(tt.profile)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMIGProfile(%q) error = %v, wantErr %v", tt.profile, err, tt.wantErr)
			}
		})
	}
}

func TestKnownMIGProfiles(t *testing.T) {
	profiles := KnownMIGProfiles()
	if len(profiles) == 0 {
		t.Error("KnownMIGProfiles() returned empty slice")
	}
	expectedCount := len(knownProfiles)
	if len(profiles) != expectedCount {
		t.Errorf("KnownMIGProfiles() returned %d profiles, expected %d", len(profiles), expectedCount)
	}
	// Verify all returned profiles are valid
	for _, p := range profiles {
		if err := ValidateMIGProfile(p); err != nil {
			t.Errorf("KnownMIGProfiles() contains invalid profile %q: %v", p, err)
		}
	}
	// Verify sorted
	for i := 1; i < len(profiles); i++ {
		if profiles[i-1] > profiles[i] {
			t.Errorf("KnownMIGProfiles() not sorted: %q > %q", profiles[i-1], profiles[i])
		}
	}
}
