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

package karpenter

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
)

const (
	// maxPendingEventLookups bounds the deleted NodeClaims whose Events are looked up per NodePool.
	maxPendingEventLookups = 3
	// pendingEventLookupTTL bounds how long a deleted NodeClaim's Events, recorded asynchronously, are looked for.
	pendingEventLookupTTL = 5 * time.Minute
	// eventLookupInterval throttles Event lookups per NodePool, as a NodeClaim recreate loop reconciles every few seconds.
	eventLookupInterval = time.Minute
)

// failureTier ranks provisioning failures by causality: a higher tier causes the lower ones, so it is the root cause.
type failureTier int

const (
	tierNone failureTier = iota
	// tierDeletedOnly: a NodeClaim was deleted before it launched or registered, for an unknown reason.
	tierDeletedOnly
	// tierInsufficientCapacity: an InsufficientCapacityError Event, possibly caused by an earlier cloud error.
	tierInsufficientCapacity
	// tierCloudError: a cloud error on a NodeClaim condition (e.g. CreateInstanceFailed) or an AsyncProvisioningError Event.
	tierCloudError
)

// rootCause is the provisioning failure reported for a NodePool.
type rootCause struct {
	tier    failureTier
	reason  string
	message string
}

// deletedNodeClaim identifies a NodeClaim deleted before its node registered.
type deletedNodeClaim struct {
	name      string
	uid       types.UID
	deletedAt time.Time
}

// nodePoolFailures holds a NodePool's provisioning failures since one of its NodeClaims last registered.
type nodePoolFailures struct {
	uid               types.UID
	cause             *rootCause
	pendingLookups    []deletedNodeClaim
	lastEventLookupAt time.Time
}

// record keeps the first cause of the highest tier, as later attempts often fail with its consequence.
func (f *nodePoolFailures) record(c rootCause) {
	if f.cause == nil || c.tier > f.cause.tier {
		f.cause = &c
	}
}

func (f *nodePoolFailures) needsEventLookup() bool {
	return f.cause == nil || f.cause.tier < tierCloudError
}

// provisioningFailureTracker remembers, in memory and per NodePool from ProvisionNodes to DeleteNodes, the root cause
// of NodeClaim provisioning failures seen by the NodeClaim watch, so it outlives the deleted NodeClaims.
type provisioningFailureTracker struct {
	mu    sync.Mutex
	now   func() time.Time
	pools map[string]*nodePoolFailures
}

func newProvisioningFailureTracker() *provisioningFailureTracker {
	return &provisioningFailureTracker{now: time.Now, pools: map[string]*nodePoolFailures{}}
}

// track starts or keeps tracking the NodePool with the given name and UID.
func (t *provisioningFailureTracker) track(nodePoolName string, uid types.UID) {
	if nodePoolName == "" || uid == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if f := t.pools[nodePoolName]; f == nil || f.uid != uid {
		t.pools[nodePoolName] = &nodePoolFailures{uid: uid}
	}
}

// untrack forgets the NodePool with the given name.
func (t *provisioningFailureTracker) untrack(nodePoolName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pools, nodePoolName)
}

func ownedByNodePool(nc *karpenterv1.NodeClaim, uid types.UID) bool {
	for _, ref := range nc.OwnerReferences {
		if ref.Kind == "NodePool" && ref.UID == uid {
			return true
		}
	}
	return false
}

// observe records a NodeClaim change; oldNC is nil unless it is an update, and deleted marks the final state.
func (t *provisioningFailureTracker) observe(oldNC, newNC *karpenterv1.NodeClaim, deleted bool) {
	if newNC == nil {
		return
	}
	nodePoolName := newNC.Labels[karpenterv1.NodePoolLabelKey]
	registered := nodeclaim.IsConditionTrue(newNC, karpenterv1.ConditionTypeRegistered)
	wasRegistered := oldNC != nil && nodeclaim.IsConditionTrue(oldNC, karpenterv1.ConditionTypeRegistered)
	launched := nodeclaim.IsConditionTrue(newNC, karpenterv1.ConditionTypeLaunched)
	reason, message, failed := nodeclaim.LifecycleFailure(newNC)

	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.pools[nodePoolName]
	if f == nil || !ownedByNodePool(newNC, f.uid) {
		return
	}

	if registered {
		// Only a new registration clears the failures, as a launch can still fail asynchronously.
		if !deleted && !wasRegistered {
			f.cause = nil
			f.pendingLookups = nil
		}
		return
	}

	if failed {
		f.record(rootCause{tier: tierCloudError, reason: nodeclaim.ValidConditionReason(reason), message: message})
	}
	if !deleted {
		return
	}
	if launched {
		f.record(rootCause{tier: tierDeletedOnly, reason: NodeClaimRegistrationFailedReason,
			message: fmt.Sprintf("NodeClaim %s was deleted after it launched but before its node registered", newNC.Name)})
	} else {
		f.record(rootCause{tier: tierDeletedOnly, reason: NodeClaimLaunchFailedReason,
			message: fmt.Sprintf("NodeClaim %s was deleted before it launched", newNC.Name)})
	}
	if !f.needsEventLookup() {
		return
	}
	for _, d := range f.pendingLookups {
		if d.uid == newNC.UID {
			return
		}
	}
	f.pendingLookups = append(f.pendingLookups, deletedNodeClaim{name: newNC.Name, uid: newNC.UID, deletedAt: t.now()})
	if len(f.pendingLookups) > maxPendingEventLookups {
		f.pendingLookups = f.pendingLookups[1:]
	}
}

// rootCause returns the NodePool's root cause since one of its NodeClaims last registered.
func (t *provisioningFailureTracker) rootCause(nodePoolName string) (rootCause, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.pools[nodePoolName]
	if f == nil || f.cause == nil {
		return rootCause{}, false
	}
	return *f.cause, true
}

// eventLookupCandidates returns the deleted NodeClaims whose Events to look up now, while no cloud error is known.
func (t *provisioningFailureTracker) eventLookupCandidates(nodePoolName string) []deletedNodeClaim {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.pools[nodePoolName]
	if f == nil {
		return nil
	}
	now := t.now()
	kept := f.pendingLookups[:0]
	for _, d := range f.pendingLookups {
		if now.Sub(d.deletedAt) < pendingEventLookupTTL {
			kept = append(kept, d)
		}
	}
	f.pendingLookups = kept
	if len(f.pendingLookups) == 0 || !f.needsEventLookup() || now.Sub(f.lastEventLookupAt) < eventLookupInterval {
		return nil
	}
	f.lastEventLookupAt = now
	return append([]deletedNodeClaim(nil), f.pendingLookups...)
}

// recordEventLookup records a found failure Event of a still pending NodeClaim; otherwise its lookup is retried later.
func (t *provisioningFailureTracker) recordEventLookup(nodePoolName string, d deletedNodeClaim, reason, message string, found bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.pools[nodePoolName]
	if f == nil {
		return
	}
	for i := range f.pendingLookups {
		if f.pendingLookups[i].uid != d.uid {
			continue
		}
		if found {
			f.pendingLookups = append(f.pendingLookups[:i], f.pendingLookups[i+1:]...)
			f.record(rootCause{tier: eventTier(reason), reason: nodeclaim.ValidConditionReason(reason), message: message})
		}
		return
	}
}
