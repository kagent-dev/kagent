package seed

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/service"
)

func TestParse_Empty(t *testing.T) {
	specs, err := Parse("", "")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if specs != nil {
		t.Errorf("Parse() = %v, want nil", specs)
	}
}

func TestParse_Inline(t *testing.T) {
	specs, err := Parse(`[{"key":"team","columns":["Todo","Done"]}]`, "")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("len(specs) = %d, want 1", len(specs))
	}
	if specs[0].Key != "team" || len(specs[0].Columns) != 2 {
		t.Errorf("spec = %+v, want key=team with 2 columns", specs[0])
	}
}

func TestParse_FilePrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "boards.json")
	if err := os.WriteFile(path, []byte(`[{"key":"fromfile","columns":["A"]}]`), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	// Inline is provided too, but the file takes precedence.
	specs, err := Parse(`[{"key":"inline","columns":["B"]}]`, path)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(specs) != 1 || specs[0].Key != "fromfile" {
		t.Errorf("specs = %+v, want the file's board to win", specs)
	}
}

func TestParse_BadJSON(t *testing.T) {
	if _, err := Parse(`not json`, ""); err == nil {
		t.Fatal("Parse() expected error for bad JSON, got nil")
	}
}

// fakeUpserter records the requests passed to UpsertBoard.
type fakeUpserter struct {
	reqs []service.CreateBoardRequest
}

func (f *fakeUpserter) UpsertBoard(_ context.Context, req service.CreateBoardRequest) (*service.Board, error) {
	f.reqs = append(f.reqs, req)
	return &service.Board{Key: req.Key}, nil
}

func TestApply_UpsertsEach(t *testing.T) {
	specs := []BoardSpec{
		{Key: "a", Columns: []string{"X"}},
		{Key: "b", Name: "B", Columns: []string{"Y", "Z"}},
	}
	f := &fakeUpserter{}
	if err := Apply(context.Background(), f, specs); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(f.reqs) != 2 {
		t.Fatalf("UpsertBoard called %d times, want 2", len(f.reqs))
	}
	if f.reqs[0].Key != "a" || f.reqs[1].Key != "b" {
		t.Errorf("upserted keys = %q,%q, want a,b", f.reqs[0].Key, f.reqs[1].Key)
	}
}
