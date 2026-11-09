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
	appsv1 "k8s.io/api/apps/v1"
)

// ContainerImage returns the image of the inference container in the StatefulSet's pod template.
// The inference container is identified by having the same name as the StatefulSet itself.
// Falls back to the first container if no name match is found.
func ContainerImage(ss *appsv1.StatefulSet) string {
	for i := range ss.Spec.Template.Spec.Containers {
		if ss.Spec.Template.Spec.Containers[i].Name == ss.Name {
			return ss.Spec.Template.Spec.Containers[i].Image
		}
	}
	if len(ss.Spec.Template.Spec.Containers) == 0 {
		return ""
	}
	return ss.Spec.Template.Spec.Containers[0].Image
}
