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

package estimator

import (
	"context"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

// NodesEstimator is an interface for estimating the number of nodes required for a workspace.
type NodesEstimator interface {
	// Name returns the name of the nodes estimator.
	Name() string

	// EstimateNodeCount estimates the number of nodes required for the given workspace.
	EstimateNodeCount(ctx context.Context, workspace *kaitov1beta1.Workspace) (int32, error)
}
