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

package version

import (
	"fmt"
	"runtime"
)

var (
	// Build-time variables injected via -ldflags
	Version   = "dev"             // Will be replaced with actual version (e.g., git tag)
	BuildDate = "unknown"         // Will be replaced with actual build timestamp
	GoVersion = runtime.Version() // Keep as runtime value - this is appropriate
)

func VersionInfo() string {
	return fmt.Sprintf("%s (Build Date: %s, Go Version: %s)", Version, BuildDate, GoVersion)
}
