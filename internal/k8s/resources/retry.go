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

package resources

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// objectPtr constrains PT to a pointer to T that implements client.Object.
type objectPtr[T any] interface {
	*T
	client.Object
}

// UpdateWithRetry reads the latest object for key, applies modify, and updates
// it, retrying on conflict.
func UpdateWithRetry[T any, PT objectPtr[T]](ctx context.Context, c client.Client, key client.ObjectKey, modify func(PT) error) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		obj := PT(new(T))
		if err := c.Get(ctx, key, obj); err != nil {
			return err
		}
		if err := modify(obj); err != nil {
			return err
		}
		return c.Update(ctx, obj)
	})
}

// UpdateStatusWithRetry reads the latest object for key, applies modify, and
// updates its status subresource. It retries on conflict and transient server
// errors, and returns nil if the object no longer exists. A nil modify writes
// the latest status back unchanged.
func UpdateStatusWithRetry[T any, PT objectPtr[T]](ctx context.Context, c client.Client, key client.ObjectKey, modify func(PT) error) error {
	return retry.OnError(retry.DefaultRetry,
		func(err error) bool {
			return apierrors.IsServiceUnavailable(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || apierrors.IsConflict(err)
		},
		func() error {
			obj := PT(new(T))
			if err := c.Get(ctx, key, obj); err != nil {
				return client.IgnoreNotFound(err)
			}
			if modify != nil {
				if err := modify(obj); err != nil {
					return err
				}
			}
			return c.Status().Update(ctx, obj)
		})
}
