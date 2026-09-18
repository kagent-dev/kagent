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

// toAgentInstanceShare decodes a share and rejects disagreement between its payload and
// indexed identity, instance, or permission.
func toAgentInstanceShare(row agentInstanceShareRow) (*apiv1alpha1.AgentInstanceShare, error) {
	share := &apiv1alpha1.AgentInstanceShare{}
	if err := proto.Unmarshal(row.Data, share); err != nil {
		return nil, fmt.Errorf("decode AgentInstance share %s: %w", row.ID, err)
	}
	if share.GetId() != row.ID.String() || share.GetAgentInstanceId() != row.InstanceID.String() ||
		share.GetPermission().String() != row.Permission {
		return nil, fmt.Errorf("AgentInstance share %s payload disagrees with indexed columns", row.ID)
	}
	return share, nil
}

// CreateAgentInstanceShare stores a share with the supplied ID, permission, and token hash
// for an instance owned by userID and sets its creation time. A missing or unowned
// instance returns ErrNotFound. Callers authorize sharing and generate the token;
// the plaintext token is never stored.
func (c *Client) CreateAgentInstanceShare(ctx context.Context, share *apiv1alpha1.AgentInstanceShare, tokenHash []byte, userID string) (*apiv1alpha1.AgentInstanceShare, error) {
	if share == nil {
		return nil, fmt.Errorf("missing AgentInstance share")
	}
	value := proto.Clone(share).(*apiv1alpha1.AgentInstanceShare)
	value.CreatedAt = timestamppb.Now()
	data, err := proto.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode AgentInstance share: %w", err)
	}
	row, err := queryOne(ctx, c.db, `
		INSERT INTO agent_instance_share (id, instance_id, permission, token_hash, data)
		SELECT $1, id, $3, $4, $5 FROM agent_instance WHERE id = $2 AND user_id = $6
		RETURNING id, instance_id, permission, data
	`,
		pgx.RowToStructByNameLax[agentInstanceShareRow], value.Id,
		value.AgentInstanceId,
		value.Permission.String(), tokenHash, data, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("create AgentInstance share: %w", notFoundOr(err))
	}
	return toAgentInstanceShare(row)
}

// GetAgentInstanceShareByTokenHash resolves a token digest to its share and the instance
// owner's ID, or ErrNotFound. Callers apply the share's permission when granting access.
func (c *Client) GetAgentInstanceShareByTokenHash(ctx context.Context, tokenHash []byte) (*apiv1alpha1.AgentInstanceShare, string, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT s.id, s.instance_id, s.permission, s.data, i.user_id AS owner_user_id
		FROM agent_instance_share s
		JOIN agent_instance i ON i.id = s.instance_id
		WHERE s.token_hash = $1
	`, pgx.RowToStructByName[agentInstanceShareRow], tokenHash)
	if err != nil {
		return nil, "", fmt.Errorf("get AgentInstance share by token: %w", notFoundOr(err))
	}
	share, err := toAgentInstanceShare(row)
	if err != nil {
		return nil, "", err
	}
	return share, *row.OwnerUserID, nil
}

// ListAgentInstanceShares returns shares only for an instance owned by userID, in
// ascending ID order after afterID, up to limit. A missing or unowned instance yields an
// empty page.
func (c *Client) ListAgentInstanceShares(ctx context.Context, instanceID, userID, afterID string, limit int) ([]*apiv1alpha1.AgentInstanceShare, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT s.id, s.instance_id, s.permission, s.data FROM agent_instance_share s
		JOIN agent_instance i ON i.id = s.instance_id
		WHERE s.instance_id = $1 AND i.user_id = $2
		  AND (NULLIF($3::text, '') IS NULL OR s.id > NULLIF($3::text, '')::uuid)
		ORDER BY s.id
		LIMIT $4
	`,
		pgx.RowToStructByNameLax[agentInstanceShareRow], instanceID, userID, afterID, int32(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list AgentInstance shares: %w", err)
	}
	result := make([]*apiv1alpha1.AgentInstanceShare, 0, len(rows))
	for _, row := range rows {
		share, err := toAgentInstanceShare(row)
		if err != nil {
			return nil, err
		}
		result = append(result, share)
	}
	return result, nil
}

// DeleteAgentInstanceShare revokes a share only when its instance belongs to userID. A
// missing or unowned share returns ErrNotFound.
func (c *Client) DeleteAgentInstanceShare(ctx context.Context, id, userID string) error {
	count, err := c.db.Exec(ctx, `
		DELETE FROM agent_instance_share s
		USING agent_instance i
		WHERE s.id = $1
		  AND i.id = s.instance_id AND i.user_id = $2
	`, id, userID)
	if err != nil {
		return fmt.Errorf("delete AgentInstance share %s: %w", id, err)
	}
	if count.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type agentInstanceShareRow struct {
	ID         uuid.UUID
	InstanceID uuid.UUID
	Permission string
	Data       []byte
	// Only token resolution joins the owner; other queries omit this column.
	OwnerUserID *string
}
