package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PushSigningState records the latest committed activation for an issuer.
// Other replicas may still sign with their previous key during local staging.
type PushSigningState struct {
	KeyID    string `db:"last_activated_key_id"`
	Revision int64
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

// LoadOrInitializePushSigningState creates the issuer's initial activation only
// if none exists. Concurrent startup never overwrites an existing activation.
func (c *Client) LoadOrInitializePushSigningState(ctx context.Context, issuer, initialKeyID string) (PushSigningState, error) {
	var state PushSigningState
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		if err := execSQL(ctx, tx, `
            INSERT INTO push_signing_state (issuer, last_activated_key_id)
            VALUES ($1, $2) ON CONFLICT (issuer) DO NOTHING
        `, issuer, initialKeyID); err != nil {
			return err
		}
		var err error
		state, err = readPushSigningState(ctx, tx, issuer)
		return err
	})
	if err != nil {
		return PushSigningState{}, fmt.Errorf("failed to initialize push signing state: %w", err)
	}
	return state, nil
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
