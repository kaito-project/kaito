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
	"context"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// nodeClaimEventNamespace holds the Events of cluster-scoped objects such as NodeClaims.
	nodeClaimEventNamespace = metav1.NamespaceDefault

	eventFieldInvolvedObjectKind = "involvedObject.kind"
	eventFieldType               = "type"

	// asyncProvisioningEventReason is published by the Azure provider when an accepted instance fails, before it
	// deletes the NodeClaim.
	asyncProvisioningEventReason = "AsyncProvisioningError"
	// insufficientCapacityEventReason is published by Karpenter before it deletes a NodeClaim that failed with an
	// insufficient capacity error. On Azure, it carries the regional vCPU quota error or, once the provider cached the
	// offering of an earlier failure as unavailable, a no-offering "all requested instance types were unavailable" error.
	// The other Azure quota and allocation errors are only recorded on the NodeClaim's Launched condition.
	insufficientCapacityEventReason = "InsufficientCapacityError"

	// nodeClaimEventListInterval bounds how often the NodeClaim Events are listed, for all NodePools.
	nodeClaimEventListInterval = 2 * time.Minute
	// nodeClaimEventWindow is how long a provisioning failure Event is reported, twice the list interval.
	nodeClaimEventWindow = 2 * nodeClaimEventListInterval

	// maxGeneratedNameBaseLen and generatedNameSuffixLen describe the API server's generateName, which Karpenter uses
	// to name NodeClaims after their NodePool: the prefix is truncated so that the random suffix fits in 63 characters.
	maxGeneratedNameBaseLen = 58
	generatedNameSuffixLen  = 5
)

type nodeClaimEvent struct {
	nodeClaimName string
	reason        string
	message       string
	lastSeen      time.Time
}

// nodeClaimEventCache holds the recent provisioning failure Events of all NodeClaims, listed at most once per
// nodeClaimEventListInterval and shared by all NodePools.
type nodeClaimEventCache struct {
	mu       sync.Mutex
	listedAt time.Time
	events   []nodeClaimEvent
}

// recent returns the provisioning failure Events seen within nodeClaimEventWindow of the last list, listing them again
// once nodeClaimEventListInterval has passed. A failed list keeps the previous Events until the next interval.
func (c *nodeClaimEventCache) recent(ctx context.Context, cl client.Client, now time.Time) []nodeClaimEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.listedAt.IsZero() && now.Sub(c.listedAt) < nodeClaimEventListInterval {
		return c.events
	}
	c.listedAt = now

	list := &corev1.EventList{}
	if err := cl.List(ctx, list,
		client.InNamespace(nodeClaimEventNamespace),
		client.MatchingFieldsSelector{Selector: fields.AndSelectors(
			fields.OneTermEqualSelector(eventFieldInvolvedObjectKind, "NodeClaim"),
			fields.OneTermEqualSelector(eventFieldType, corev1.EventTypeWarning),
		)},
	); err != nil {
		klog.V(4).InfoS("failed to list NodeClaim events", "err", err)
		return c.events
	}
	events := []nodeClaimEvent{}
	for i := range list.Items {
		e := &list.Items[i]
		if e.Type != corev1.EventTypeWarning || eventTier(e.Reason, e.Message) == tierNotAFailure {
			continue
		}
		lastSeen := eventLastSeen(e)
		if now.Sub(lastSeen) > nodeClaimEventWindow {
			continue
		}
		events = append(events, nodeClaimEvent{
			nodeClaimName: e.InvolvedObject.Name, reason: e.Reason, message: e.Message, lastSeen: lastSeen,
		})
	}
	c.events = events
	return events
}

// failureEventTier ranks a provisioning failure Event: an asynchronous provisioning error or an insufficient capacity
// error that says why, such as an exhausted regional quota, carries the cloud error that the no-offering insufficient
// capacity errors of later attempts are a consequence of. Those retries fail within seconds once the provider cached
// the offering as unavailable, so the newest Event alone would hide the cause.
type failureEventTier int

const (
	tierNotAFailure failureEventTier = iota
	tierNoOffering
	tierInsufficientCapacity
	tierAsyncProvisioning
)

func eventTier(reason, message string) failureEventTier {
	switch reason {
	case asyncProvisioningEventReason:
		return tierAsyncProvisioning
	case insufficientCapacityEventReason:
		// A NodeClass that cannot be resolved is read directly instead, so its Events are not reported.
		if isNodeClassResolutionFailure(reason, message) {
			return tierNotAFailure
		}
		if isNoOfferingAvailable(reason, message) {
			return tierNoOffering
		}
		return tierInsufficientCapacity
	default:
		return tierNotAFailure
	}
}

// eventLastSeen returns when the Event last occurred, as repeated Events only update their series or lastTimestamp.
func eventLastSeen(e *corev1.Event) time.Time {
	last := e.CreationTimestamp.Time
	for _, t := range []time.Time{e.FirstTimestamp.Time, e.LastTimestamp.Time, e.EventTime.Time} {
		if t.After(last) {
			last = t
		}
	}
	if e.Series != nil && e.Series.LastObservedTime.After(last) {
		last = e.Series.LastObservedTime.Time
	}
	return last
}

// isNodePoolNodeClaim reports whether the NodeClaim name was generated from the NodePool name. The suffix length
// excludes the NodeClaims of NodePools whose name starts with this NodePool's name followed by a dash.
func isNodePoolNodeClaim(nodeClaimName, nodePoolName string) bool {
	base := nodePoolName + "-"
	if len(base) > maxGeneratedNameBaseLen {
		base = base[:maxGeneratedNameBaseLen]
	}
	suffix, ok := strings.CutPrefix(nodeClaimName, base)
	return ok && len(suffix) == generatedNameSuffixLen && !strings.Contains(suffix, "-")
}

// nodePoolFailureEvent returns the provisioning failure Event of the NodePool's NodeClaims that occurred after since,
// preferring an asynchronous provisioning error, then an insufficient capacity error that says why, then a no-offering
// one, then the latest.
func nodePoolFailureEvent(events []nodeClaimEvent, nodePoolName string, since time.Time) (nodeClaimEvent, bool) {
	var best *nodeClaimEvent
	for i := range events {
		e := &events[i]
		if !e.lastSeen.After(since) || !isNodePoolNodeClaim(e.nodeClaimName, nodePoolName) {
			continue
		}
		if best == nil || eventTier(e.reason, e.message) > eventTier(best.reason, best.message) ||
			eventTier(e.reason, e.message) == eventTier(best.reason, best.message) && e.lastSeen.After(best.lastSeen) {
			best = e
		}
	}
	if best == nil {
		return nodeClaimEvent{}, false
	}
	return *best, true
}
