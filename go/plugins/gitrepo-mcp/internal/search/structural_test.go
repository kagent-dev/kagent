package search

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/plugins/gitrepo-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/gitrepo-mcp/internal/storage"
)

// --- parseAstGrepOutput tests ---

func TestParseAstGrepOutput_Empty(t *testing.T) {
	results, err := parseAstGrepOutput(nil, "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("want 0 results, got %d", len(results))
	}
}

func TestParseAstGrepOutput_SingleMatch(t *testing.T) {
	jsonLine := `{"text":"func hello() error","range":{"start":{"line":10,"column":0},"end":{"line":12,"column":1}},"file":"/tmp/repo/main.go","language":"Go"}`

	results, err := parseAstGrepOutput([]byte(jsonLine), "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}

	r := results[0]
	if r.FilePath != "main.go" {
		t.Errorf("filePath: want main.go, got %s", r.FilePath)
	}
	if r.LineStart != 11 { // 0-indexed → 1-indexed
		t.Errorf("lineStart: want 11, got %d", r.LineStart)
	}
	if r.LineEnd != 13 {
		t.Errorf("lineEnd: want 13, got %d", r.LineEnd)
	}
	if r.Content != "func hello() error" {
		t.Errorf("content: want 'func hello() error', got %q", r.Content)
	}
	if r.Language != "Go" {
		t.Errorf("language: want Go, got %s", r.Language)
	}
}

func TestParseAstGrepOutput_MultipleMatches(t *testing.T) {
	lines := `{"text":"func a()","range":{"start":{"line":0,"column":0},"end":{"line":2,"column":1}},"file":"/repo/a.go","language":"Go"}
{"text":"func b()","range":{"start":{"line":5,"column":0},"end":{"line":7,"column":1}},"file":"/repo/b.go","language":"Go"}
{"text":"func c()","range":{"start":{"line":10,"column":0},"end":{"line":12,"column":1}},"file":"/repo/sub/c.go","language":"Go"}`

	results, err := parseAstGrepOutput([]byte(lines), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}

	if results[0].FilePath != "a.go" {
		t.Errorf("result[0] filePath: want a.go, got %s", results[0].FilePath)
	}
	if results[1].FilePath != "b.go" {
		t.Errorf("result[1] filePath: want b.go, got %s", results[1].FilePath)
	}
	if results[2].FilePath != "sub/c.go" {
		t.Errorf("result[2] filePath: want sub/c.go, got %s", results[2].FilePath)
	}
}

func TestParseAstGrepOutput_InvalidJSON(t *testing.T) {
	_, err := parseAstGrepOutput([]byte("not json"), "/tmp/repo")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseAstGrepOutput_BlankLines(t *testing.T) {
	input := "\n\n" + `{"text":"func a()","range":{"start":{"line":0,"column":0},"end":{"line":0,"column":10}},"file":"/repo/a.go","language":"Go"}` + "\n\n"

	results, err := parseAstGrepOutput([]byte(input), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
}

func TestParseAstGrepOutput_FilePathStripping(t *testing.T) {
	// Test with trailing slash in repoPath
	jsonLine := `{"text":"x","range":{"start":{"line":0,"column":0},"end":{"line":0,"column":1}},"file":"/data/repos/myrepo/src/main.go","language":"Go"}`

	results, err := parseAstGrepOutput([]byte(jsonLine), "/data/repos/myrepo")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].FilePath != "src/main.go" {
		t.Errorf("filePath: want src/main.go, got %s", results[0].FilePath)
	}
}

func TestParseAstGrepOutput_DifferentLanguages(t *testing.T) {
	lines := `{"text":"def hello():","range":{"start":{"line":0,"column":0},"end":{"line":1,"column":8}},"file":"/repo/hello.py","language":"Python"}
{"text":"fn main()","range":{"start":{"line":0,"column":0},"end":{"line":3,"column":1}},"file":"/repo/main.rs","language":"Rust"}`

	results, err := parseAstGrepOutput([]byte(lines), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].Language != "Python" {
		t.Errorf("result[0] language: want Python, got %s", results[0].Language)
	}
	if results[1].Language != "Rust" {
		t.Errorf("result[1] language: want Rust, got %s", results[1].Language)
	}
}

// --- SupportedLanguages tests ---

func TestSupportedLanguages(t *testing.T) {
	langs := SupportedLanguages()
	if len(langs) == 0 {
		t.Fatal("expected non-empty language list")
	}

	// Check required languages are present
	required := []string{"go", "python", "javascript", "typescript", "rust", "java"}
	for _, r := range required {
		found := false
		for _, l := range langs {
			if l == r {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in supported languages", r)
		}
	}
}

// --- AstSearcher validation tests (using in-memory DB) ---

func setupTestAstSearcher(t *testing.T) (*AstSearcher, *storage.RepoStore) {
	t.Helper()

	dir := t.TempDir()
	cfg := &config.Config{
		DBType:  config.DBTypeSQLite,
		DBPath:  filepath.Join(dir, "test.db"),
		DataDir: dir,
	}
	mgr, err := storage.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Initialize(); err != nil {
		t.Fatal(err)
	}

	repoStore := storage.NewRepoStore(mgr.DB())
	return NewAstSearcher(repoStore), repoStore
}

func TestAstSearcher_EmptyPattern(t *testing.T) {
	s, _ := setupTestAstSearcher(t)
	_, err := s.Search("", "test-repo", "")
	if err == nil {
		t.Error("expected error for empty pattern")
	}
}

func TestAstSearcher_RepoNotFound(t *testing.T) {
	s, _ := setupTestAstSearcher(t)
	_, err := s.Search("func $NAME()", "nonexistent", "")
	if err == nil {
		t.Error("expected error for missing repo")
	}
}

func TestAstSearcher_RepoNotReady(t *testing.T) {
	s, repoStore := setupTestAstSearcher(t)

	repo := &storage.Repo{
		Name:      "cloning-repo",
		URL:       "https://example.com/cloning",
		Branch:    "main",
		Status:    storage.RepoStatusCloning,
		LocalPath: "/tmp/cloning",
	}
	if err := repoStore.Create(repo); err != nil {
		t.Fatal(err)
	}

	_, err := s.Search("func $NAME()", "cloning-repo", "")
	if err == nil {
		t.Error("expected error for repo in cloning state")
	}
}

func TestAstSearcher_AcceptsClonedRepo(t *testing.T) {
	s, repoStore := setupTestAstSearcher(t)

	now := time.Now()
	repo := &storage.Repo{
		Name:       "cloned-repo",
		URL:        "https://example.com/cloned",
		Branch:     "main",
		Status:     storage.RepoStatusCloned,
		LocalPath:  "/tmp/nonexistent-for-test",
		LastSynced: &now,
	}
	if err := repoStore.Create(repo); err != nil {
		t.Fatal(err)
	}

	// This will fail at the ast-grep exec level (not found or bad path), not at validation
	_, err := s.Search("func $NAME()", "cloned-repo", "")
	if err == nil {
		// ast-grep probably not installed in test env — that's fine
		return
	}
	// Acceptable errors: binary not found or exec failure on nonexistent path
	// Unacceptable: validation errors about repo status
	if err.Error() == "repo cloned-repo is not ready (status: cloned)" {
		t.Error("should accept cloned repos for ast search")
	}
}

func TestAstSearcher_AcceptsIndexedRepo(t *testing.T) {
	s, repoStore := setupTestAstSearcher(t)

	now := time.Now()
	repo := &storage.Repo{
		Name:        "indexed-repo",
		URL:         "https://example.com/indexed",
		Branch:      "main",
		Status:      storage.RepoStatusIndexed,
		LocalPath:   "/tmp/nonexistent-for-test",
		LastSynced:  &now,
		LastIndexed: &now,
	}
	if err := repoStore.Create(repo); err != nil {
		t.Fatal(err)
	}

	_, err := s.Search("func $NAME()", "indexed-repo", "")
	if err == nil {
		return
	}
	if err.Error() == "repo indexed-repo is not ready (status: indexed)" {
		t.Error("should accept indexed repos for ast search")
	}
}

// --- isExecNotFound tests ---

func TestIsExecNotFound(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"executable file not found in $PATH", true},
		{"exec: \"ast-grep\": executable file not found in $PATH", true},
		{"some other error", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isExecNotFound(errors.New(tt.msg))
		if got != tt.want {
			t.Errorf("isExecNotFound(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}
