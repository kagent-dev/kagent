package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// toSessionShare decodes a share and rejects disagreement between its payload and
// indexed identity, session, or permission.
func toSessionShare(row sessionShareRow) (*apiv1alpha1.SessionShare, error) {
	share := &apiv1alpha1.SessionShare{}
	if err := proto.Unmarshal(row.Data, share); err != nil {
		return nil, fmt.Errorf("decode Session share %s: %w", row.ID, err)
	}
	if share.GetId() != row.ID.String() || share.GetSessionId() != row.SessionID.String() ||
		share.GetPermission().String() != row.Permission {
		return nil, fmt.Errorf("session share %s payload disagrees with indexed columns", row.ID)
	}
	return share, nil
}

// CreateSessionShare stores a share with the supplied ID, permission, and token hash
// for a session owned by userID and sets its creation time. A missing or unowned
// session returns ErrNotFound. Callers authorize sharing and generate the token;
// the plaintext token is never stored. Insertion locks the live session so
// concurrent deletion either revokes this share or prevents its creation.
func (c *Client) CreateSessionShare(ctx context.Context, share *apiv1alpha1.SessionShare, tokenHash []byte, userID string) (*apiv1alpha1.SessionShare, error) {
	if share == nil {
		return nil, fmt.Errorf("missing Session share")
	}
	value := proto.Clone(share).(*apiv1alpha1.SessionShare)
	value.CreatedAt = timestamppb.Now()
	data, err := proto.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Session share: %w", err)
	}
	row, err := queryOne(ctx, c.db, `
		INSERT INTO session_share (id, session_id, permission, token_hash, data)
		SELECT $1, id, $3, $4, $5 FROM session
		WHERE id = $2 AND user_id = $6 AND state <> 'SESSION_STATE_DELETED'
		FOR UPDATE
		RETURNING id, session_id, permission, data
	`,
		pgx.RowToStructByNameLax[sessionShareRow], value.Id,
		value.SessionId,
		value.Permission.String(), tokenHash, data, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("create Session share: %w", notFoundOr(err))
	}
	return toSessionShare(row)
}

// GetSessionShareByTokenHash resolves a token digest to its share and the session
// owner's ID, or ErrNotFound. Callers apply the share's permission when granting access.
func (c *Client) GetSessionShareByTokenHash(ctx context.Context, tokenHash []byte) (*apiv1alpha1.SessionShare, string, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT s.id, s.session_id, s.permission, s.data, i.user_id AS owner_user_id
		FROM session_share s
		JOIN session i ON i.id = s.session_id
		WHERE s.token_hash = $1 AND i.state <> 'SESSION_STATE_DELETED'
	`, pgx.RowToStructByName[sessionShareRow], tokenHash)
	if err != nil {
		return nil, "", fmt.Errorf("get Session share by token: %w", notFoundOr(err))
	}
	share, err := toSessionShare(row)
	if err != nil {
		return nil, "", err
	}
	return share, *row.OwnerUserID, nil
}

// ListSessionShares returns shares only for a session owned by userID, in
// ascending ID order after afterID, up to limit. A missing or unowned session yields an
// empty page.
func (c *Client) ListSessionShares(ctx context.Context, sessionID, userID, afterID string, limit int) ([]*apiv1alpha1.SessionShare, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT s.id, s.session_id, s.permission, s.data FROM session_share s
		JOIN session i ON i.id = s.session_id
		WHERE s.session_id = $1 AND i.user_id = $2 AND i.state <> 'SESSION_STATE_DELETED'
		  AND (NULLIF($3::text, '') IS NULL OR s.id > NULLIF($3::text, '')::uuid)
		ORDER BY s.id
		LIMIT $4
	`,
		pgx.RowToStructByNameLax[sessionShareRow], sessionID, userID, afterID, int32(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list Session shares: %w", err)
	}
	result := make([]*apiv1alpha1.SessionShare, 0, len(rows))
	for _, row := range rows {
		share, err := toSessionShare(row)
		if err != nil {
			return nil, err
		}
		result = append(result, share)
	}
	return result, nil
}

// DeleteSessionShare revokes a share only when its session belongs to userID. A
// missing or unowned share returns ErrNotFound.
func (c *Client) DeleteSessionShare(ctx context.Context, id, userID string) error {
	count, err := c.db.Exec(ctx, `
		DELETE FROM session_share s
		USING session i
		WHERE s.id = $1
		  AND i.id = s.session_id AND i.user_id = $2
	`, id, userID)
	if err != nil {
		return fmt.Errorf("delete Session share %s: %w", id, err)
	}
	if count.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type sessionShareRow struct {
	ID         uuid.UUID
	SessionID  uuid.UUID
	Permission string
	Data       []byte
	// Only token resolution joins the owner; other queries omit this column.
	OwnerUserID *string
}
