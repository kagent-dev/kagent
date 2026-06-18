package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	dbgen "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db/gen"
)

// Board is the JSON-facing metadata for a kanban board: its key, display name,
// ordered column set, and scope/owner. It does not carry tasks (see BoardState).
type Board struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Scope       string    `json:"scope"`
	Owner       string    `json:"owner,omitempty"`
	Columns     []string  `json:"columns"`
	Subtasks    []string  `json:"subtasks,omitempty"` // checklist template auto-added to new Tasks
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateBoardRequest is the input for CreateBoard / UpsertBoard. Columns is
// required and must contain at least one non-empty column. Name defaults to Key
// and Scope defaults to "general" when empty.
type CreateBoardRequest struct {
	Key         string
	Name        string
	Description string
	Scope       string
	Owner       string
	Columns     []string
	Subtasks    []string // optional checklist template auto-added to new Tasks
}

// normalize validates and fills defaults for a board request, returning the
// cleaned key/name/scope, the trimmed, de-duplicated column list, and the
// (optional) trimmed, de-duplicated subtask template.
func (req CreateBoardRequest) normalize() (key, name, scope string, columns, subtasks []string, err error) {
	key = strings.TrimSpace(req.Key)
	if key == "" {
		return "", "", "", nil, nil, fmt.Errorf("board key is required")
	}

	columns = normalizeColumns(req.Columns)
	if len(columns) == 0 {
		return "", "", "", nil, nil, fmt.Errorf("board %q must define at least one column", key)
	}

	// The subtask template is optional; reuse the column cleaner to trim, drop
	// empties, and de-duplicate.
	subtasks = normalizeColumns(req.Subtasks)

	name = strings.TrimSpace(req.Name)
	if name == "" {
		name = key
	}

	scope = strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = db.BoardScopeGeneral
	}
	if !db.ValidScope(scope) {
		return "", "", "", nil, nil, fmt.Errorf("invalid board scope %q, valid scopes are %q and %q",
			scope, db.BoardScopeGeneral, db.BoardScopeAgent)
	}

	return key, name, scope, columns, subtasks, nil
}

// ListBoards returns all boards ordered by creation time.
func (s *TaskService) ListBoards(ctx context.Context) ([]*Board, error) {
	rows, err := s.q.ListBoards(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing boards: %w", err)
	}
	out := make([]*Board, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapBoard(row))
	}
	return out, nil
}

// GetBoardMeta returns a single board's metadata by key. Not-found is reported as
// a wrapped pgx.ErrNoRows.
func (s *TaskService) GetBoardMeta(ctx context.Context, key string) (*Board, error) {
	row, err := s.q.GetBoardByKey(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("getting board %q: %w", key, err)
	}
	return mapBoard(row), nil
}

// CreateBoard inserts a new board. The key must be unique; a duplicate key
// surfaces as the underlying Postgres unique-violation error.
func (s *TaskService) CreateBoard(ctx context.Context, req CreateBoardRequest) (*Board, error) {
	key, name, scope, columns, subtasks, err := req.normalize()
	if err != nil {
		return nil, err
	}

	row, err := s.q.CreateBoard(ctx, dbgen.CreateBoardParams{
		Key:         key,
		Name:        name,
		Description: req.Description,
		Scope:       scope,
		Owner:       req.Owner,
		Columns:     columns,
		Subtasks:    subtasks,
	})
	if err != nil {
		return nil, fmt.Errorf("creating board %q: %w", key, err)
	}
	return mapBoard(row), nil
}

// UpsertBoard inserts a board or, if a board with the same key already exists,
// updates its name/description/scope/owner/columns. It is used for idempotent
// seeding from configuration.
func (s *TaskService) UpsertBoard(ctx context.Context, req CreateBoardRequest) (*Board, error) {
	key, name, scope, columns, subtasks, err := req.normalize()
	if err != nil {
		return nil, err
	}

	row, err := s.q.UpsertBoard(ctx, dbgen.UpsertBoardParams{
		Key:         key,
		Name:        name,
		Description: req.Description,
		Scope:       scope,
		Owner:       req.Owner,
		Columns:     columns,
		Subtasks:    subtasks,
	})
	if err != nil {
		return nil, fmt.Errorf("upserting board %q: %w", key, err)
	}
	return mapBoard(row), nil
}

// resolveBoard fetches a board by key, defaulting an empty key to the built-in
// default board. It is the entry point for board-scoped task operations.
func (s *TaskService) resolveBoard(ctx context.Context, key string) (dbgen.Board, error) {
	if key == "" {
		key = db.DefaultBoardKey
	}
	board, err := s.q.GetBoardByKey(ctx, key)
	if err != nil {
		return dbgen.Board{}, fmt.Errorf("getting board %q: %w", key, err)
	}
	return board, nil
}

// normalizeColumns trims whitespace, drops empty entries, and removes duplicate
// columns (case-sensitive) while preserving first-seen order.
func normalizeColumns(columns []string) []string {
	out := make([]string, 0, len(columns))
	seen := make(map[string]struct{}, len(columns))
	for _, c := range columns {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// mapBoard converts a dbgen.Board row into the API Board metadata type.
func mapBoard(row dbgen.Board) *Board {
	return &Board{
		ID:          row.ID,
		Key:         row.Key,
		Name:        row.Name,
		Description: row.Description,
		Scope:       row.Scope,
		Owner:       row.Owner,
		Columns:     row.Columns,
		Subtasks:    row.Subtasks,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}
