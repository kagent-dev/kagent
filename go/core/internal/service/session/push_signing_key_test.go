package session

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/require"
)

func pemKey(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestParsePushPrivateKey(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	valid := pemKey(t, key)
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	for _, test := range []struct {
		name     string
		material []byte
		valid    bool
	}{
		{"PKCS8 Ed25519", valid, true},
		{"whitespace", append([]byte(" \n"), append(valid, '\n')...), true},
		{"empty", nil, false},
		{"seed", []byte("c3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3Nzc3M="), false},
		{"EC key", pemKey(t, ec), false},
		{"invalid DER", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("bad")}), false},
		{"public key", pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: key.Public().(ed25519.PublicKey)}), false},
		{"encrypted key", pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: key}), false},
		{"multiple blocks", append(append([]byte(nil), valid...), valid...), false},
		{"trailing data", append(append([]byte(nil), valid...), []byte("garbage")...), false},
		{"leading data", append([]byte("garbage\n"), valid...), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parsePushPrivateKey(test.material)
			if test.valid {
				require.NoError(t, err)
				require.Equal(t, key, parsed)
			} else {
				require.Error(t, err)
			}
		})
	}
}
