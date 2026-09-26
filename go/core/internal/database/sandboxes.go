package database

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sandboxRow struct {
	runtimeInstanceRow
	Data                []byte
	RequestID           string
	Namespace           string
	SandboxTemplateName string
	RevisionReceipt     string
	RequestHash         []byte
	ExpiresAt           time.Time
}

func (r sandboxRow) sandbox() (*apiv1alpha1.Sandbox, error) {
	s := &apiv1alpha1.Sandbox{}
	if err := proto.Unmarshal(r.Data, s); err != nil {
		return nil, fmt.Errorf("decode Sandbox %s: %w", r.ID, err)
	}
	state, operation, err := r.lifecycle()
	if err != nil {
		return nil, err
	}
	// Relational columns own identity, lifecycle, filtering, and expiration.
	s.Id, s.Creator = r.ID.String(), r.UserID
	s.State, s.Operation = state, operation
	s.PreparedRevision, s.ExpiresAt = r.RevisionReceipt, timestamppb.New(r.ExpiresAt)
	if s.SandboxTemplate == nil {
		s.SandboxTemplate = &apiv1alpha1.ResourceReference{}
	}
	s.SandboxTemplate.Namespace, s.SandboxTemplate.Name = r.Namespace, r.SandboxTemplateName
	return s, nil
}

// saveSandboxPayload preserves the full protobuf, including unknown fields.
// Callers hold the runtime row lock and commit lifecycle columns with this write.
func saveSandboxPayload(ctx context.Context, tx pgx.Tx, instance *apiv1alpha1.Sandbox) ([]byte, error) {
	data, err := proto.Marshal(instance)
	if err != nil {
		return nil, fmt.Errorf("encode Sandbox %s: %w", instance.Id, err)
	}
	return data, execSQL(ctx, tx, `UPDATE sandbox SET data = $2 WHERE id = $1`, instance.Id, data)
}

// SandboxCreateOptions contains creation inputs that are not part of the resource.
type SandboxCreateOptions struct {
	RequestHash []byte
	TemplateUID string
	TTL         time.Duration
}

// CreateSandbox atomically reserves a sandbox and its prepared revision. A
// duplicate creator/requestID returns the original resource when inputs match.
// The boolean reports whether this call created the sandbox.
func (c *Client) CreateSandbox(ctx context.Context, request *apiv1alpha1.Sandbox, requestID string, options SandboxCreateOptions) (*apiv1alpha1.Sandbox, bool, error) {
	existing, err := readSandboxRequest(ctx, c.db, request.GetCreator(), requestID)
	if err == nil {
		if !bytes.Equal(existing.RequestHash, options.RequestHash) {
			return nil, false, ErrIdempotencyConflict
		}
		if existing.State == "RUNTIME_STATE_DELETED" {
			return nil, false, ErrFailedPrecondition
		}
		instance, err := existing.sandbox()
		return instance, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("get Sandbox request: %w", err)
	}

	var row sandboxRow
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		row, err = insertSandbox(ctx, tx, request, requestID, options)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = readSandboxRequest(ctx, c.db, request.GetCreator(), requestID)
		if err != nil {
			return nil, false, fmt.Errorf("get concurrent Sandbox request: %w", err)
		}
		if !bytes.Equal(existing.RequestHash, options.RequestHash) {
			return nil, false, ErrIdempotencyConflict
		}
		if existing.State == "RUNTIME_STATE_DELETED" {
			return nil, false, ErrFailedPrecondition
		}
		instance, err := existing.sandbox()
		return instance, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("insert Sandbox: %w", err)
	}
	instance, err := row.sandbox()
	return instance, err == nil, err
}

func readSandboxRequest(ctx context.Context, db dbExecutor, userID, requestID string) (sandboxRow, error) {
	return queryOne(ctx, db, `SELECT * FROM sandbox_record WHERE user_id = $1 AND request_id = $2`, pgx.RowToStructByName[sandboxRow], userID, requestID)
}

// insertSandbox reserves a new sandbox in CREATING/CREATE and pins the active
// template's latest successful revision. A duplicate creator/requestID returns
// pgx.ErrNoRows so the caller can roll back and read the winning transaction.
func insertSandbox(ctx context.Context, tx pgx.Tx, request *apiv1alpha1.Sandbox, requestID string, options SandboxCreateOptions) (sandboxRow, error) {
	if options.TTL < time.Second || options.TTL > 24*time.Hour {
		return sandboxRow{}, ErrFailedPrecondition
	}
	var revision string
	var createdAt time.Time
	err := tx.QueryRow(ctx, `
		SELECT latest_successful_revision, clock_timestamp() FROM sandbox_template_definition
		WHERE namespace = $1 AND sandbox_template_name = $2 AND sandbox_template_uid = $3
		    AND retired_at IS NULL AND latest_successful_revision IS NOT NULL FOR UPDATE
	`, request.GetSandboxTemplate().GetNamespace(), request.GetSandboxTemplate().GetName(), options.TemplateUID).Scan(&revision, &createdAt)
	if err != nil {
		return sandboxRow{}, notFoundOr(err)
	}
	if err := lockRuntimeRevisionForReference(ctx, tx, revision, runtimeKindSandbox); err != nil {
		return sandboxRow{}, err
	}
	instance := proto.CloneOf(request)
	instance.PreparedRevision = revision
	instance.State = apiv1alpha1.RuntimeState_RUNTIME_STATE_CREATING
	instance.Operation = apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE
	instance.CreatedAt = timestamppb.New(createdAt)
	instance.UpdatedAt = timestamppb.New(createdAt)
	instance.ExpiresAt = timestamppb.New(createdAt.Add(options.TTL))
	instance.Guest = &apiv1alpha1.GuestCapabilities{ProcessExecution: true, FileAccess: true}
	data, err := proto.Marshal(instance)
	if err != nil {
		return sandboxRow{}, fmt.Errorf("encode Sandbox %s: %w", instance.Id, err)
	}
	if _, err := queryOne(ctx, tx, `
		INSERT INTO runtime_instance (id, kind, user_id, request_id, prepared_revision, state, operation)
		VALUES ($1, 'sandbox', $2, $3, $4, 'RUNTIME_STATE_CREATING', 'RUNTIME_OPERATION_CREATE')
		ON CONFLICT (kind, user_id, request_id) DO NOTHING RETURNING id
	`, pgx.RowTo[uuid.UUID], instance.Id, instance.Creator, requestID, revision); err != nil {
		return sandboxRow{}, err
	}
	if err := execSQL(ctx, tx, `
		INSERT INTO sandbox (id, namespace, sandbox_template_name, revision_receipt, request_hash, expires_at, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, instance.Id, instance.GetSandboxTemplate().GetNamespace(), instance.GetSandboxTemplate().GetName(), revision, options.RequestHash, instance.ExpiresAt.AsTime(), data); err != nil {
		return sandboxRow{}, err
	}
	return readSandbox(ctx, tx, instance.Id)
}

// GetSandbox includes deletion tombstones so authorized retries can observe cleanup.
func (c *Client) GetSandbox(ctx context.Context, id, userID string) (*apiv1alpha1.Sandbox, error) {
	row, err := queryOne(ctx, c.db, `SELECT * FROM sandbox_record WHERE id = $1 AND user_id = $2`, pgx.RowToStructByName[sandboxRow], id, userID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	return row.sandbox()
}

func (c *Client) ListSandboxes(ctx context.Context, userID, afterID string, limit int, template *apiv1alpha1.ResourceReference) ([]*apiv1alpha1.Sandbox, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT * FROM sandbox_record WHERE user_id = $1 AND state <> 'RUNTIME_STATE_DELETED'
		    AND ($2::text = '' OR id > NULLIF($2, '')::uuid)
		    AND ($4::text = '' OR (namespace = $4 AND sandbox_template_name = $5)) ORDER BY id LIMIT $3
	`, pgx.RowToStructByName[sandboxRow], userID, afterID, limit, template.GetNamespace(), template.GetName())
	if err != nil {
		return nil, err
	}
	result := make([]*apiv1alpha1.Sandbox, 0, len(rows))
	for _, row := range rows {
		s, err := row.sandbox()
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

func readSandbox(ctx context.Context, db dbExecutor, id string) (sandboxRow, error) {
	return queryOne(ctx, db, `SELECT * FROM sandbox_record WHERE id = $1`, pgx.RowToStructByName[sandboxRow], id)
}

func lockSandbox(ctx context.Context, tx pgx.Tx, id string) (sandboxRow, error) {
	return queryOne(ctx, tx, `SELECT * FROM sandbox_record WHERE id = $1 FOR UPDATE`, pgx.RowToStructByName[sandboxRow], id)
}

type SandboxOperation = RuntimeOperation[*apiv1alpha1.Sandbox]

func (c *Client) BeginSandboxOperation(ctx context.Context, id string, kind apiv1alpha1.RuntimeOperation) (*SandboxOperation, error) {
	var result SandboxOperation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSandbox(ctx, tx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if kind != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
			expired, err := queryOne(ctx, tx, `SELECT expires_at <= clock_timestamp() FROM sandbox WHERE id = $1`, pgx.RowTo[bool], id)
			if err != nil {
				return err
			}
			if expired {
				return ErrFailedPrecondition
			}
		}
		needed, err := sandboxOperationNeeded(row.runtimeInstanceRow, kind)
		if err != nil {
			return err
		}
		if needed {
			if row.ExecutorID != nil {
				active, err := queryOne(ctx, tx, `SELECT executor_expires_at > clock_timestamp() FROM runtime_instance WHERE id = $1`, pgx.RowTo[bool], id)
				if err != nil {
					return err
				}
				if active {
					return ErrConflict
				}
			}
			row, err = beginSandboxOperation(ctx, tx, row, kind)
			if err != nil {
				return err
			}
		}
		instance, err := row.sandbox()
		if err != nil {
			return err
		}
		result = runtimeOperationFor(row.runtimeInstanceRow, instance)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Sandbox lifecycle requests may supersede pending work after its active attempt
// ends. Operation IDs reject stale completions.
func sandboxOperationNeeded(row runtimeInstanceRow, requested apiv1alpha1.RuntimeOperation) (bool, error) {
	state, operation, err := row.lifecycle()
	if err != nil {
		return false, err
	}
	if requested < apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE || requested > apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
		return false, ErrFailedPrecondition
	}
	if state == apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED {
		if requested == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
			return false, nil
		}
		return false, ErrNotFound
	}
	if operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE && requested != operation {
		return false, ErrFailedPrecondition
	}
	if requested == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE && state != apiv1alpha1.RuntimeState_RUNTIME_STATE_CREATING {
		return false, nil
	}
	if state == apiv1alpha1.RuntimeState_RUNTIME_STATE_CREATING && requested != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE && requested != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
		return false, ErrFailedPrecondition
	}
	if operation != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return operation != requested || row.OperationID == nil, nil
	}
	switch requested {
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND:
		return state != apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED, nil
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME:
		return state != apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, nil
	default:
		return true, nil
	}
}

func beginSandboxOperation(ctx context.Context, tx pgx.Tx, row sandboxRow, kind apiv1alpha1.RuntimeOperation) (sandboxRow, error) {
	var err error
	row.runtimeInstanceRow, err = beginRuntimeOperation(ctx, tx, row.runtimeInstanceRow, runtimeKindSandbox, kind)
	if err != nil {
		return row, err
	}
	instance, err := row.sandbox()
	if err != nil {
		return row, err
	}
	instance.UpdatedAt = timestamppb.Now()
	row.Data, err = saveSandboxPayload(ctx, tx, instance)
	return row, err
}

func (c *Client) ClaimSandboxOperation(ctx context.Context, id string, operationID, executorID uuid.UUID) (bool, error) {
	return c.claimRuntimeOperation(ctx, id, runtimeKindSandbox, operationID, executorID)
}

func (c *Client) FinishSandboxOperation(ctx context.Context, id string, operationID, executorID uuid.UUID, failure string) (*apiv1alpha1.Sandbox, error) {
	var result *apiv1alpha1.Sandbox
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSandbox(ctx, tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		next, err := completeRuntimeOperation(row.runtimeInstanceRow, operationID, executorID, failure)
		if err != nil {
			return err
		}
		if err := saveRuntimeLifecycle(ctx, tx, next, runtimeKindSandbox); err != nil {
			return err
		}
		row.runtimeInstanceRow = next
		result, err = row.sandbox()
		if err != nil {
			return err
		}
		result.UpdatedAt = timestamppb.Now()
		result.Failure = nil
		if failure != "" {
			result.Failure = &apiv1alpha1.Failure{Reason: "PreparationFailed", Message: failure}
		}
		_, err = saveSandboxPayload(ctx, tx, result)
		return err
	})
	return result, err
}

func (c *Client) GetSandboxOperation(ctx context.Context, id string) (*SandboxOperation, error) {
	row, err := readSandbox(ctx, c.db, id)
	if err != nil {
		return nil, notFoundOr(err)
	}
	instance, err := row.sandbox()
	if err != nil {
		return nil, err
	}
	result := runtimeOperationFor(row.runtimeInstanceRow, instance)
	return &result, nil
}

func (c *Client) RecordSandboxOperationFailure(ctx context.Context, id string, operationID uuid.UUID) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSandbox(ctx, tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.OperationID == nil || *row.OperationID != operationID || row.Operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE.String() {
			return nil
		}
		instance, err := row.sandbox()
		if err != nil {
			return err
		}
		instance.Failure = &apiv1alpha1.Failure{Reason: "RuntimeUnavailable", Message: "Runtime operation is pending; retry the lifecycle request"}
		instance.UpdatedAt = timestamppb.Now()
		_, err = saveSandboxPayload(ctx, tx, instance)
		return err
	})
}

func (c *Client) ListExpiredSandboxes(ctx context.Context, afterID string, limit int) ([]string, error) {
	return queryMany(ctx, c.db, `SELECT s.id::text FROM sandbox s JOIN runtime_instance r USING (id)
	    WHERE r.kind = 'sandbox' AND r.state <> 'RUNTIME_STATE_DELETED'
	        AND s.expires_at <= clock_timestamp()
	        AND (NULLIF($1, '')::uuid IS NULL OR s.id > NULLIF($1, '')::uuid)
	    ORDER BY s.id LIMIT $2`, pgx.RowTo[string], afterID, limit)
}
