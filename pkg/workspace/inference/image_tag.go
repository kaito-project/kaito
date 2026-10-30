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

package inference

import (
	"fmt"

	"github.com/distribution/reference"
	"k8s.io/apimachinery/pkg/util/validation"
)

// GetImageTag returns an explicit image tag suitable for an upgrade authorization
// label. Untagged and digest-pinned images fail closed: a version label cannot
// represent a digest change, even when the reference also contains a tag.
func GetImageTag(image string) (string, error) {
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return "", fmt.Errorf("invalid image reference %q: %w", image, err)
	}
	if _, ok := ref.(reference.Digested); ok {
		return "", fmt.Errorf("image %q is digest-pinned; upgrade authorization requires a tag-only reference", image)
	}
	tagged, ok := ref.(reference.Tagged)
	if !ok {
		return "", fmt.Errorf("image %q has no explicit tag", image)
	}
	tag := tagged.Tag()
	if errs := validation.IsValidLabelValue(tag); len(errs) > 0 {
		return "", fmt.Errorf("image tag %q is not a valid upgrade label: %v", tag, errs)
	}
	return tag, nil
}
