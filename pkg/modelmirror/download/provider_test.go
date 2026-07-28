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

package download

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestWorkloadIdentityPodLabels(t *testing.T) {
	t.Run("azure returns the WI label", func(t *testing.T) {
		labels := WorkloadIdentityPodLabels(consts.AzureCloudName)
		assert.Equal(t, "true", labels["azure.workload.identity/use"])
	})

	t.Run("non-azure returns nil", func(t *testing.T) {
		assert.Nil(t, WorkloadIdentityPodLabels("aws"))
		assert.Nil(t, WorkloadIdentityPodLabels(""))
	})
}
