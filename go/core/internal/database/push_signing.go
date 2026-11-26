package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PushSigningState records the latest committed activation for an issuer.
// Other replicas may still sign with their previous key during local staging.
type PushSigningState struct {
	KeyID    string `db:"last_activated_key_id"`
	Revision int64
}

// PushJWK is the public Ed25519 verification key exposed by the issuer's JWKS.
type PushJWK struct {
	KeyType string `json:"kty"`
	Curve   string `json:"crv"`
	Alg     string `json:"alg"`
	Use     string `json:"use"`
	KeyID   string `json:"kid"`
	X       string `json:"x"`
}

type PublishedPushKey struct {
	JWK         PushJWK   `json:"jwk"`
	RetainUntil time.Time `json:"retain_until"`
}

type PushSigningSnapshot struct {
	PushSigningState
	PublishedKeys map[string]PublishedPushKey `db:"published_keys"`
}

// PublishPushSigningKeys renews this replica's keys and returns the issuer's
// merged keys and activation atomically. Only the first writer initializes the
// activation. Later publications only extend deadlines and prune expired keys.
func (c *Client) PublishPushSigningKeys(ctx context.Context, issuer, initialKeyID string, local []PushJWK, retainUntil time.Time) (PushSigningSnapshot, error) {
	// Match the precision of the timestamptz comparisons used to merge deadlines.
	retainUntil = retainUntil.UTC().Truncate(time.Microsecond)
	renewals := make(map[string]PublishedPushKey, len(local))
	for _, key := range local {
		renewals[key.KeyID] = PublishedPushKey{JWK: key, RetainUntil: retainUntil}
	}
	snapshot, err := queryOne(ctx, c.db, `
        INSERT INTO push_signing_state AS state (issuer, last_activated_key_id, published_keys)
        VALUES ($1, $2, $3)
        ON CONFLICT (issuer) DO UPDATE SET published_keys = (
            SELECT COALESCE(jsonb_object_agg(key, value), '{}'::jsonb) FROM (
                SELECT DISTINCT ON (key) key, value FROM (
                    SELECT key, value FROM jsonb_each(state.published_keys)
                    UNION ALL
                    SELECT key, value FROM jsonb_each(EXCLUDED.published_keys)
                ) AS candidates
                WHERE (value->>'retain_until')::timestamptz > statement_timestamp()
                ORDER BY key, (value->>'retain_until')::timestamptz DESC
            ) AS retained
        )
        RETURNING last_activated_key_id, revision, published_keys
    `, pgx.RowToStructByName[PushSigningSnapshot], issuer, initialKeyID, renewals)
	if err != nil {
		return PushSigningSnapshot{}, fmt.Errorf("failed to publish push signing keys for issuer %q: %w", issuer, err)
	}
	return snapshot, nil
}

// IsPushSigningKeyActive reports whether keyID is the issuer's latest activation.
// An issuer without an activation returns false and remains uninitialized.
func (c *Client) IsPushSigningKeyActive(ctx context.Context, issuer, keyID string) (bool, error) {
	state, err := readPushSigningState(ctx, c.db, issuer)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read push signing state for issuer %q: %w", issuer, err)
	}
	return state.KeyID == keyID, nil
}

// ActivatePushSigningKey commits a new key only at the observed revision.
// Reconfirming the same key succeeds at any revision without advancing it.
// A superseded attempt returns the latest state unchanged; a missing issuer is
// an error. Callers must compare the returned key before beginning to sign.
func (c *Client) ActivatePushSigningKey(ctx context.Context, issuer, keyID string, revision int64) (PushSigningState, error) {
	var state PushSigningState
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		state, err = queryOne(ctx, tx, `
            UPDATE push_signing_state SET
                revision = CASE WHEN last_activated_key_id = $2 THEN revision ELSE revision + 1 END,
                last_activated_key_id = $2
            WHERE issuer = $1 AND (last_activated_key_id = $2 OR revision = $3)
            RETURNING last_activated_key_id, revision
        `, pgx.RowToStructByName[PushSigningState], issuer, keyID, revision)
		if errors.Is(err, pgx.ErrNoRows) {
			state, err = readPushSigningState(ctx, tx, issuer)
		}
		return err
	})
	if err != nil {
		return PushSigningState{}, fmt.Errorf("failed to activate push signing key: %w", err)
	}
	return state, nil
}

// readPushSigningState reads the issuer's latest activation, returning pgx.ErrNoRows if absent.
func readPushSigningState(ctx context.Context, db dbExecutor, issuer string) (PushSigningState, error) {
	return queryOne(ctx, db, `
        SELECT last_activated_key_id, revision FROM push_signing_state WHERE issuer = $1
    `, pgx.RowToStructByName[PushSigningState], issuer)
}
