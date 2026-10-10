package jwtadmission

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

const (
	// keysMaxAge is how long fetched keys are trusted before the JWKS is read
	// again, so a key the issuer withdrew stops admitting.
	keysMaxAge = 10 * time.Minute
	// refreshInterval bounds how often a grant with an unknown key refetches
	// the JWKS, so unknown key ids cannot hammer the issuer.
	refreshInterval = 10 * time.Second
	// jwksMaxBytes bounds the JWKS document.
	jwksMaxBytes = 1 << 20
)

var (
	errUnknownKey      = errors.New("unknown signing key")
	errKeysUnavailable = errors.New("JWKS unavailable")
)

// keySet is the issuer's published verification keys by key id, fetched on
// first use, refetched when they are older than keysMaxAge or a grant names a
// key id they lack, so a rotation needs no restart.
type keySet struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu          sync.Mutex
	keys        map[string]crypto.PublicKey
	fetchedAt   time.Time
	lastAttempt time.Time
	lastErr     error
}

func newKeySet(url string, client *http.Client) *keySet {
	return &keySet{url: url, client: client, now: time.Now}
}

// key returns the verification key kid names. The lock is held across the
// fetch on purpose: concurrent grants wait for one fetch instead of each
// starting their own.
func (k *keySet) key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	if key, known := k.keys[kid]; known && now.Sub(k.fetchedAt) < keysMaxAge {
		return key, nil
	}
	if !k.lastAttempt.IsZero() && now.Sub(k.lastAttempt) < refreshInterval {
		if k.lastErr != nil {
			return nil, fmt.Errorf("%w: %w", errKeysUnavailable, k.lastErr)
		}
		return nil, errUnknownKey
	}
	k.lastAttempt = now
	keys, err := k.fetch(ctx)
	k.lastErr = err
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errKeysUnavailable, err)
	}
	k.keys, k.fetchedAt = keys, now
	if key, known := keys[kid]; known {
		return key, nil
	}
	return nil, errUnknownKey
}

// jwk is one key of an RFC 7517 key set, EC or RSA.
type jwk struct {
	KeyType string `json:"kty"`
	KeyID   string `json:"kid"`
	Use     string `json:"use"`
	Curve   string `json:"crv"`
	X       string `json:"x"`
	Y       string `json:"y"`
	N       string `json:"n"`
	E       string `json:"e"`
}

func (k *keySet) fetch(ctx context.Context) (map[string]crypto.PublicKey, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := k.client.Do(request)
	if err != nil {
		if ctx.Err() == nil {
			// The client's own timeout wraps context.DeadlineExceeded, which
			// would report an issuer that never answers as the caller's
			// deadline; it is the keys that are unavailable.
			return nil, fmt.Errorf("fetch JWKS: %v", err)
		}
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS answered %s", response.Status)
	}
	var document struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, jwksMaxBytes)).Decode(&document); err != nil {
		return nil, fmt.Errorf("decode JWKS: %w", err)
	}
	keys := make(map[string]crypto.PublicKey, len(document.Keys))
	for _, published := range document.Keys {
		if published.KeyID == "" || (published.Use != "" && published.Use != "sig") {
			continue
		}
		// A key this verifier cannot read admits nothing; the others still do.
		if key, err := published.publicKey(); err == nil {
			keys[published.KeyID] = key
		}
	}
	return keys, nil
}

func (k jwk) publicKey() (crypto.PublicKey, error) {
	switch k.KeyType {
	case "EC":
		var curve elliptic.Curve
		switch k.Curve {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		default:
			return nil, fmt.Errorf("unsupported curve %q", k.Curve)
		}
		x, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, err
		}
		y, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil {
			return nil, err
		}
		size := (curve.Params().BitSize + 7) / 8
		if len(x) != size || len(y) != size {
			return nil, errors.New("EC coordinates have the wrong length")
		}
		return ecdsa.ParseUncompressedPublicKey(curve, append(append([]byte{4}, x...), y...))
	case "RSA":
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, err
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, err
		}
		exponent := new(big.Int).SetBytes(e)
		if len(n) < 256 || !exponent.IsInt64() || exponent.Int64() < 3 || exponent.Int64() > 1<<31-1 {
			return nil, errors.New("RSA key is shorter than 2048 bits or has an unusable exponent")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent.Int64())}, nil
	default:
		return nil, fmt.Errorf("unsupported key type %q", k.KeyType)
	}
}
