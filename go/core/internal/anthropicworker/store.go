package anthropicworker

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

const leaseDuration = 30 * time.Second

//go:embed migrations/*.sql
var migrationFiles embed.FS

var errLeaseLost = errors.New("assignment lease lost")

func MigrationSource(schema string) migrations.Source {
	return migrations.Source{
		Name: "anthropic sandbox adapter", Schema: schema, TrackingTable: "anthropic_schema_migrations",
		FS: migrationFiles, Dir: "migrations",
	}
}

type store struct{ db *pgxpool.Pool }

type assignment struct {
	id, environment, sessionID, lease string
	credential                        []byte
	acknowledged                      bool
	heartbeat, sandboxID              string
	generation                        int64
}

type command struct {
	id, name, phase, processID, result string
	input                              json.RawMessage
	isError                            bool
	startedAt                          time.Time
}

func credentialCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("credential encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}

func (s store) enqueue(ctx context.Context, w assignment) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO sandbox_work (id, environment, session_id, credential)
		VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`, w.id, w.environment, w.sessionID, w.credential)
	return err
}

func (s store) claim(ctx context.Context, environments []string) (assignment, error) {
	w := assignment{lease: uuid.NewString()}
	err := s.db.QueryRow(ctx, `
		WITH next AS (
			SELECT id FROM sandbox_work
			WHERE finished_at IS NULL AND environment = ANY($3) AND available_at <= clock_timestamp()
				AND (lease_until IS NULL OR lease_until <= clock_timestamp())
			ORDER BY available_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		UPDATE sandbox_work w SET lease_token = $1, lease_until = clock_timestamp() + $2 * interval '1 second'
		FROM next WHERE w.id = next.id
		RETURNING w.id, w.environment, w.session_id, w.credential, w.acknowledged, w.heartbeat,
			w.sandbox_id, w.sandbox_generation`, w.lease, int(leaseDuration.Seconds()), environments).
		Scan(&w.id, &w.environment, &w.sessionID, &w.credential, &w.acknowledged, &w.heartbeat, &w.sandboxID, &w.generation)
	return w, err
}

const owned = `id = $1 AND lease_token = $2 AND lease_until > clock_timestamp()`

func requireUpdate(tag pgconn.CommandTag, err error) error {
	if err == nil && tag.RowsAffected() == 0 {
		return errLeaseLost
	}
	return err
}

func (s store) renew(ctx context.Context, w assignment) error {
	return requireUpdate(s.db.Exec(ctx, `
		UPDATE sandbox_work SET lease_until = clock_timestamp() + $3 * interval '1 second' WHERE `+owned,
		w.id, w.lease, int(leaseDuration.Seconds())))
}

func (s store) release(ctx context.Context, w assignment, delay time.Duration) error {
	return requireUpdate(s.db.Exec(ctx, `
		UPDATE sandbox_work SET lease_token = NULL, lease_until = NULL,
			available_at = clock_timestamp() + $3 * interval '1 millisecond' WHERE `+owned,
		w.id, w.lease, delay.Milliseconds()))
}

func (s store) ack(ctx context.Context, w assignment) error {
	return requireUpdate(s.db.Exec(ctx, `UPDATE sandbox_work SET acknowledged = true WHERE `+owned, w.id, w.lease))
}

func (s store) heartbeat(ctx context.Context, w assignment, token string) error {
	return requireUpdate(s.db.Exec(ctx, `UPDATE sandbox_work SET heartbeat = $3 WHERE `+owned, w.id, w.lease, token))
}

func (s store) saveSandbox(ctx context.Context, w *assignment, id string) error {
	err := s.db.QueryRow(ctx, `
		UPDATE sandbox_work SET sandbox_id = $3, sandbox_generation = sandbox_generation + CASE WHEN $3 = '' THEN 1 ELSE 0 END
		WHERE `+owned+` RETURNING sandbox_generation`, w.id, w.lease, id).Scan(&w.generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return errLeaseLost
	}
	if err == nil {
		w.sandboxID = id
	}
	return err
}

func (s store) sandbox(ctx context.Context, w assignment) (string, error) {
	var id string
	err := s.db.QueryRow(ctx, `SELECT sandbox_id FROM sandbox_work WHERE `+owned, w.id, w.lease).Scan(&id)
	return id, err
}

func (s store) finish(ctx context.Context, w assignment) error {
	return requireUpdate(s.db.Exec(ctx, `
		WITH finished AS (
			UPDATE sandbox_work SET finished_at = clock_timestamp(), credential = NULL, heartbeat = '',
				lease_token = NULL, lease_until = NULL WHERE `+owned+` RETURNING id
		), purged AS (
			DELETE FROM sandbox_command WHERE work_id IN (SELECT id FROM finished)
		) SELECT id FROM finished`, w.id, w.lease))
}

// Lock the assignment row for the duration of a command write so a takeover
// cannot commit between checking ownership and modifying its journal.
const commandOwner = `WITH owner AS (SELECT id FROM sandbox_work WHERE ` + owned + ` FOR SHARE) `

func (s store) addCommand(ctx context.Context, w assignment, c command) error {
	_, err := s.db.Exec(ctx, commandOwner+`
		INSERT INTO sandbox_command (work_id, id, name, input)
		SELECT id, $3, $4, $5 FROM owner ON CONFLICT (work_id, id) DO NOTHING`,
		w.id, w.lease, c.id, c.name, c.input)
	return err
}

func (s store) nextCommand(ctx context.Context, w assignment) (command, error) {
	var c command
	err := s.db.QueryRow(ctx, `
		SELECT c.id, c.name, c.input, c.phase, coalesce(c.process_id, ''), coalesce(c.result, ''), c.is_error, c.started_at
		FROM sandbox_command c WHERE c.work_id IN (SELECT id FROM sandbox_work WHERE `+owned+`)
			AND c.phase <> 'done'
		ORDER BY c.sequence LIMIT 1`, w.id, w.lease).
		Scan(&c.id, &c.name, &c.input, &c.phase, &c.processID, &c.result, &c.isError, &c.startedAt)
	return c, err
}

func (s store) transition(ctx context.Context, w assignment, c command, phase string, processID, result *string, isError bool) error {
	return requireUpdate(s.db.Exec(ctx, commandOwner+`
		UPDATE sandbox_command SET phase = CASE WHEN discard AND $5 = 'result' THEN 'done' ELSE $5 END,
			process_id = $6, result = CASE WHEN discard THEN NULL ELSE $7 END, is_error = $8,
			started_at = CASE WHEN $5 = 'starting' THEN clock_timestamp() ELSE started_at END,
			input = CASE WHEN $5 = 'done' OR (discard AND $5 = 'result') THEN NULL ELSE input END
		WHERE work_id IN (SELECT id FROM owner) AND id = $3 AND phase = $4`,
		w.id, w.lease, c.id, c.phase, phase, processID, result, isError))
}

func (s store) settle(ctx context.Context, w assignment, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, commandOwner+`
		UPDATE sandbox_command SET phase = 'done', input = NULL, process_id = NULL, result = NULL
		WHERE work_id IN (SELECT id FROM owner) AND id = ANY($3) AND phase <> 'done'`, w.id, w.lease, ids)
	return err
}

func (s store) interrupt(ctx context.Context, w assignment, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, commandOwner+`
		UPDATE sandbox_command SET discard = true,
			phase = CASE WHEN phase IN ('starting', 'running') THEN phase ELSE 'done' END,
			input = CASE WHEN phase IN ('starting', 'running') THEN input END, result = NULL
		WHERE work_id IN (SELECT id FROM owner) AND id = ANY($3) AND phase <> 'done'`, w.id, w.lease, ids)
	return err
}
