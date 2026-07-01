// Package seed parses board definitions from configuration (an inline JSON string
// or a JSON file, the file taking precedence) and upserts them into the database
// at startup. Seeding is idempotent: redeploys reconcile board names and columns
// via UpsertBoard, so the same Helm values can be applied repeatedly.
package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
)

// BoardSpec is one board definition as provided by configuration (Helm values
// rendered to JSON). It mirrors service.CreateBoardRequest in JSON form.
type BoardSpec struct {
	Key         string   `json:"key"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	Columns     []string `json:"columns"`
	Subtasks    []string `json:"subtasks,omitempty"`
}

// Upserter is the subset of service.TaskService that seeding needs.
type Upserter interface {
	UpsertBoard(ctx context.Context, req service.CreateBoardRequest) (*service.Board, error)
}

// Parse resolves the board specs from the inline JSON and/or file. The file path
// takes precedence over the inline value. An empty source yields no specs and no
// error.
func Parse(inline, file string) ([]BoardSpec, error) {
	data := strings.TrimSpace(inline)
	if file != "" {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading boards file %q: %w", file, err)
		}
		data = strings.TrimSpace(string(content))
	}
	if data == "" {
		return nil, nil
	}

	var specs []BoardSpec
	if err := json.Unmarshal([]byte(data), &specs); err != nil {
		return nil, fmt.Errorf("parsing board definitions: %w", err)
	}
	return specs, nil
}

// Apply upserts each spec via the Upserter. It returns the first error
// encountered.
func Apply(ctx context.Context, u Upserter, specs []BoardSpec) error {
	for _, s := range specs {
		if _, err := u.UpsertBoard(ctx, service.CreateBoardRequest{
			Key:         s.Key,
			Name:        s.Name,
			Description: s.Description,
			Scope:       s.Scope,
			Owner:       s.Owner,
			Columns:     s.Columns,
			Subtasks:    s.Subtasks,
		}); err != nil {
			return fmt.Errorf("seeding board %q: %w", s.Key, err)
		}
	}
	return nil
}
