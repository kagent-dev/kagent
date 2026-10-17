package postgres

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolConfigHidesInvalidURL(t *testing.T) {
	_, err := PoolConfig("postgres://user:password@localhost:5432/database?sslmode=disable", "kagent_owner")
	require.NoError(t, err)

	_, err = PoolConfig("postgres://user:do-not-disclose@[invalid", "kagent_owner")
	require.Error(t, err)
	assert.ErrorIs(t, err, errInvalidURL)
	assert.NotContains(t, err.Error(), "do-not-disclose")
}

func TestPoolConfigRefreshesTLSForLiteralURL(t *testing.T) {
	config, err := PoolConfig("postgres://user:static-password@database:5432/app?sslmode=require", "kagent_owner")
	require.NoError(t, err)
	require.NotNil(t, config.BeforeConnect)

	connConfig := config.ConnConfig.Copy()
	initialTLS := connConfig.TLSConfig
	connConfig.Password = "unchanged-password"
	require.NoError(t, config.BeforeConnect(context.Background(), connConfig))

	assert.Equal(t, "unchanged-password", connConfig.Password)
	assert.NotSame(t, initialTLS, connConfig.TLSConfig)
}

func TestPoolConfigHooks(t *testing.T) {
	config, err := PoolConfig("postgres://user:password@database:5432/app?sslmode=disable", "kagent_owner")
	require.NoError(t, err)
	assert.Nil(t, config.BeforeConnect)
	assert.NotNil(t, config.AfterConnect)
}

func TestPoolConfigRequiresRole(t *testing.T) {
	_, err := PoolConfig("postgres://user:password@database:5432/app?sslmode=disable", "")
	require.ErrorContains(t, err, "PostgreSQL role must not be empty")
}

// Pod Certificates rotate in place, so each new connection must present the
// bundle as it is on disk, not as it was when the pool was configured.
func TestPoolConfigReloadsRotatedClientCertificate(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "credential-bundle.pem")
	first := writeClientBundle(t, bundle, "first")
	config, err := PoolConfig("postgres://user@database:5432/app?sslmode=require&sslcert="+bundle+"&sslkey="+bundle, "kagent_owner")
	require.NoError(t, err)
	require.Equal(t, first, presentedCertificate(t, config.ConnConfig.TLSConfig.Certificates))

	second := writeClientBundle(t, bundle, "second")
	connConfig := config.ConnConfig.Copy()
	require.NoError(t, config.BeforeConnect(context.Background(), connConfig))
	require.Equal(t, second, presentedCertificate(t, connConfig.TLSConfig.Certificates))

	require.NoError(t, os.Remove(bundle))
	require.ErrorIs(t, config.BeforeConnect(context.Background(), config.ConnConfig.Copy()), errInvalidURL)
}

// writeClientBundle writes a key followed by its self-signed certificate, the
// layout of a Pod Certificate credential bundle, and returns the certificate.
func writeClientBundle(t *testing.T, path, commonName string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	cert, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	data := append(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})...)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return cert
}

func presentedCertificate(t *testing.T, certificates []tls.Certificate) []byte {
	t.Helper()
	require.Len(t, certificates, 1)
	return certificates[0].Certificate[0]
}
