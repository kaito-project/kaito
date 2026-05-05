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

// Package cert provides a TLS GetCertificate callback that loads the webhook
// serving certificate from a Kubernetes Secret via an informer-backed
// SecretLister, with ResourceVersion-keyed caching.
//
// It exists so KAITO's webhook server can start its TCP listener immediately
// on pod start, without waiting for cert-controller to have written cert files
// to disk. controller-runtime's webhook server installs its own
// certwatcher.New only when tls.Config.GetCertificate is nil at Start time;
// supplying a GetCertificate via webhook.Options.TLSOpts therefore bypasses
// the eager file read that would otherwise crash the manager when the cert
// Secret has not yet been populated.
package cert

import (
	"crypto/tls"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// NewServerCertLoader returns a TLS GetCertificate callback that memoises the
// parsed certificate keyed on the source Secret's ResourceVersion. This avoids
// reparsing PEM bytes on every TLS handshake while still picking up rotations
// automatically (a new ResourceVersion invalidates the cache).
//
// certDataKey / keyDataKey name the fields inside secret.Data that hold the
// PEM-encoded certificate and key. For cert-controller-managed Secrets these
// are "tls.crt" and "tls.key".
//
// Contract: returning (nil, nil) when the Secret is not yet present surfaces
// to the TLS client as "tls: no certificates configured". Crucially, the TCP
// listener stays up; only the handshake fails. This is the desired behaviour
// during the brief window between pod start and cert-controller writing the
// Secret.
func NewServerCertLoader(lister corev1listers.SecretLister, namespace, secretName, certDataKey, keyDataKey string) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	var (
		mu         sync.Mutex
		cachedRV   string
		cachedCert *tls.Certificate
	)

	return func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		secret, err := lister.Secrets(namespace).Get(secretName)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// Sentinel: TLS server reports "no certificates configured"
				// and closes the connection cleanly. Listener stays up.
				return nil, nil //nolint:nilerr
			}
			return nil, fmt.Errorf("failed to get secret %s/%s: %w", namespace, secretName, err)
		}

		rv := secret.ResourceVersion
		mu.Lock()
		defer mu.Unlock()
		if rv != "" && rv == cachedRV && cachedCert != nil {
			return cachedCert, nil
		}

		certPEM, ok := secret.Data[certDataKey]
		if !ok {
			return nil, fmt.Errorf("certificate field %q not found in secret %s/%s", certDataKey, namespace, secretName)
		}
		keyPEM, ok := secret.Data[keyDataKey]
		if !ok {
			return nil, fmt.Errorf("key field %q not found in secret %s/%s", keyDataKey, namespace, secretName)
		}
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("failed to create X509 key pair: %w", err)
		}

		cachedCert = &cert
		cachedRV = rv
		return cachedCert, nil
	}
}
