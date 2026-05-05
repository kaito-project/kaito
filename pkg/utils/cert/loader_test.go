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

package cert_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	corev1listers "k8s.io/client-go/listers/core/v1"

	"github.com/kaito-project/kaito/pkg/utils/cert"
)

// fakeSecretLister is a hand-rolled corev1listers.SecretLister keyed by
// namespace/name. It avoids pulling client-go's testing fixtures into the
// dependency tree just for these unit tests.
type fakeSecretLister struct {
	mu      sync.RWMutex
	secrets map[string]*corev1.Secret // key = "namespace/name"
}

func newFakeSecretLister() *fakeSecretLister {
	return &fakeSecretLister{secrets: map[string]*corev1.Secret{}}
}

func (f *fakeSecretLister) set(ns, name, rv string, data map[string][]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secrets[ns+"/"+name] = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: rv,
		},
		Data: data,
	}
}

func (f *fakeSecretLister) List(_ labels.Selector) ([]*corev1.Secret, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]*corev1.Secret, 0, len(f.secrets))
	for _, s := range f.secrets {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSecretLister) Secrets(ns string) corev1listers.SecretNamespaceLister {
	return &fakeSecretNamespaceLister{parent: f, ns: ns}
}

type fakeSecretNamespaceLister struct {
	parent *fakeSecretLister
	ns     string
}

func (f *fakeSecretNamespaceLister) List(_ labels.Selector) ([]*corev1.Secret, error) {
	f.parent.mu.RLock()
	defer f.parent.mu.RUnlock()
	out := make([]*corev1.Secret, 0)
	for k, s := range f.parent.secrets {
		if k[:len(f.ns)+1] == f.ns+"/" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSecretNamespaceLister) Get(name string) (*corev1.Secret, error) {
	f.parent.mu.RLock()
	defer f.parent.mu.RUnlock()
	s, ok := f.parent.secrets[f.ns+"/"+name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "secrets"}, name)
	}
	return s, nil
}

// makeSelfSignedCertPEM generates a fresh ECDSA P-256 self-signed certificate.
// A new key pair is produced per call, so callers can rely on pointer equality
// of the returned bytes implying "same cert".
func makeSelfSignedCertPEM(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "webhookcert-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

const (
	testNS         = "kaito-system"
	testSecretName = "workspace-webhook-cert"
	certKey        = "tls.crt"
	keyKey         = "tls.key"
)

// TestNewServerCertLoader_MissingSecretReturnsNil documents the sentinel
// behaviour that keeps the listener up before cert-controller has populated
// the Secret.
func TestNewServerCertLoader_MissingSecretReturnsNil(t *testing.T) {
	lister := newFakeSecretLister()
	load := cert.NewServerCertLoader(lister, testNS, testSecretName, certKey, keyKey)

	cert, err := load(nil)
	require.NoError(t, err)
	assert.Nil(t, cert)
}

func TestNewServerCertLoader_MalformedCertReturnsError(t *testing.T) {
	lister := newFakeSecretLister()
	lister.set(testNS, testSecretName, "1", map[string][]byte{
		certKey: []byte("not a real cert"),
		keyKey:  []byte("not a real key"),
	})
	load := cert.NewServerCertLoader(lister, testNS, testSecretName, certKey, keyKey)

	cert, err := load(nil)
	require.Error(t, err)
	assert.Nil(t, cert)
}

func TestNewServerCertLoader_MissingCertField(t *testing.T) {
	lister := newFakeSecretLister()
	lister.set(testNS, testSecretName, "1", map[string][]byte{
		"other": []byte("x"),
	})
	load := cert.NewServerCertLoader(lister, testNS, testSecretName, certKey, keyKey)

	_, err := load(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), certKey)
}

func TestNewServerCertLoader_MissingKeyField(t *testing.T) {
	certPEM, _ := makeSelfSignedCertPEM(t)
	lister := newFakeSecretLister()
	lister.set(testNS, testSecretName, "1", map[string][]byte{
		certKey: certPEM,
	})
	load := cert.NewServerCertLoader(lister, testNS, testSecretName, certKey, keyKey)

	_, err := load(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), keyKey)
}

// TestNewServerCertLoader_CacheHitOnSameRV proves the loader does not reparse
// PEM when the Secret's ResourceVersion is unchanged: even if the underlying
// data has been mutated to garbage, the cached pointer is returned.
func TestNewServerCertLoader_CacheHitOnSameRV(t *testing.T) {
	certPEM, keyPEM := makeSelfSignedCertPEM(t)
	lister := newFakeSecretLister()
	lister.set(testNS, testSecretName, "rv-1", map[string][]byte{
		certKey: certPEM,
		keyKey:  keyPEM,
	})
	load := cert.NewServerCertLoader(lister, testNS, testSecretName, certKey, keyKey)

	first, err := load(nil)
	require.NoError(t, err)
	require.NotNil(t, first)

	// Mutate underlying data to garbage but keep the same RV.
	lister.set(testNS, testSecretName, "rv-1", map[string][]byte{
		certKey: []byte("garbage"),
		keyKey:  []byte("garbage"),
	})

	second, err := load(nil)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Same(t, first, second, "same RV must return the cached *tls.Certificate")
}

// TestNewServerCertLoader_CacheMissOnRVBump proves the loader picks up
// rotations: a fresh ResourceVersion forces a reparse and yields a new
// pointer.
func TestNewServerCertLoader_CacheMissOnRVBump(t *testing.T) {
	cert1PEM, key1PEM := makeSelfSignedCertPEM(t)
	cert2PEM, key2PEM := makeSelfSignedCertPEM(t)

	lister := newFakeSecretLister()
	lister.set(testNS, testSecretName, "rv-1", map[string][]byte{
		certKey: cert1PEM,
		keyKey:  key1PEM,
	})
	load := cert.NewServerCertLoader(lister, testNS, testSecretName, certKey, keyKey)

	first, err := load(nil)
	require.NoError(t, err)
	require.NotNil(t, first)

	lister.set(testNS, testSecretName, "rv-2", map[string][]byte{
		certKey: cert2PEM,
		keyKey:  key2PEM,
	})

	second, err := load(nil)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.NotSame(t, first, second, "RV bump must invalidate the cache")
}
