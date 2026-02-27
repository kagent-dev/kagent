package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/embedder"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/indexer"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/search"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
)

func setupTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		DBType:  config.DBTypeSQLite,
		DBPath:  filepath.Join(tmpDir, "test.db"),
		DataDir: tmpDir,
	}
	mgr, err := storage.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Initialize(); err != nil {
		t.Fatal(err)
	}

	repoStore := storage.NewRepoStore(mgr.DB())
	embStore := storage.NewEmbeddingStore(mgr.DB())
	emb := embedder.NewHashEmbedder(768)

	reposDir := filepath.Join(tmpDir, "repos")
	idx := indexer.NewIndexer(repoStore, embStore, emb)
	s := search.NewSearcher(repoStore, embStore, emb)
	astS := search.NewAstSearcher(repoStore)

	srv := NewServer(repoStore, idx, s, astS, reposDir)
	ts := httptest.NewServer(srv.Handler())

	return srv, ts
}

func doRequest(t *testing.T, method, url string, body interface{}) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

// --- Health ---

func TestHealth(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "GET", ts.URL+"/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	decodeJSON(t, resp, &body)
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %s", body["status"])
	}
}

// --- List repos ---

func TestListRepos_Empty(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "GET", ts.URL+"/api/repos", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body listReposResponse
	decodeJSON(t, resp, &body)
	if len(body.Repos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(body.Repos))
	}
}

func TestListRepos_WithRepos(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	_ = srv.repoStore.Create(&storage.Repo{
		Name: "alpha", URL: "http://example.com/alpha.git", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: "/tmp/alpha",
	})
	_ = srv.repoStore.Create(&storage.Repo{
		Name: "beta", URL: "http://example.com/beta.git", Branch: "dev",
		Status: storage.RepoStatusIndexed, LocalPath: "/tmp/beta",
	})

	resp := doRequest(t, "GET", ts.URL+"/api/repos", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body listReposResponse
	decodeJSON(t, resp, &body)
	if len(body.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(body.Repos))
	}
	if body.Repos[0].Name != "alpha" {
		t.Errorf("expected first repo name=alpha, got %s", body.Repos[0].Name)
	}
}

// --- Add repo ---

func TestAddRepo_MissingName(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/repos", map[string]string{"url": "http://example.com/repo.git"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAddRepo_MissingURL(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/repos", map[string]string{"name": "test"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAddRepo_InvalidJSON(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/repos", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAddRepo_Accepted(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/repos", addRepoRequest{
		Name: "test", URL: "http://invalid-url/repo.git", Branch: "main",
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202, got %d", resp.StatusCode)
	}

	var repo storage.Repo
	decodeJSON(t, resp, &repo)
	if repo.Name != "test" {
		t.Errorf("expected name=test, got %s", repo.Name)
	}
	if repo.Status != storage.RepoStatusCloning {
		t.Errorf("expected status=cloning, got %s", repo.Status)
	}
}

func TestAddRepo_DefaultBranch(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/repos", map[string]string{
		"name": "test", "url": "http://example.com/repo.git",
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202, got %d", resp.StatusCode)
	}

	var repo storage.Repo
	decodeJSON(t, resp, &repo)
	if repo.Branch != "main" {
		t.Errorf("expected branch=main, got %s", repo.Branch)
	}
}

func TestAddRepo_Duplicate(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	_ = srv.repoStore.Create(&storage.Repo{
		Name: "test", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: "/tmp/test",
	})

	resp := doRequest(t, "POST", ts.URL+"/api/repos", addRepoRequest{
		Name: "test", URL: "http://example.com/repo.git",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}
}

// --- Get repo ---

func TestGetRepo_Found(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	_ = srv.repoStore.Create(&storage.Repo{
		Name: "kagent", URL: "http://example.com/kagent.git", Branch: "main",
		Status: storage.RepoStatusIndexed, LocalPath: "/tmp/kagent", FileCount: 42, ChunkCount: 100,
	})

	resp := doRequest(t, "GET", ts.URL+"/api/repos/kagent", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var repo storage.Repo
	decodeJSON(t, resp, &repo)
	if repo.Name != "kagent" {
		t.Errorf("expected name=kagent, got %s", repo.Name)
	}
	if repo.FileCount != 42 {
		t.Errorf("expected fileCount=42, got %d", repo.FileCount)
	}
}

func TestGetRepo_NotFound(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "GET", ts.URL+"/api/repos/nonexistent", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// --- Delete repo ---

func TestDeleteRepo_Success(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	_ = srv.repoStore.Create(&storage.Repo{
		Name: "test", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: t.TempDir(),
	})

	resp := doRequest(t, "DELETE", ts.URL+"/api/repos/test", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify deleted
	resp2 := doRequest(t, "GET", ts.URL+"/api/repos/test", nil)
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

func TestDeleteRepo_NotFound(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "DELETE", ts.URL+"/api/repos/nonexistent", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// --- Sync repo ---

func TestSyncRepo_NotFound(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/repos/nonexistent/sync", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSyncRepo_Busy(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	_ = srv.repoStore.Create(&storage.Repo{
		Name: "busy", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusCloning, LocalPath: "/tmp/busy",
	})

	resp := doRequest(t, "POST", ts.URL+"/api/repos/busy/sync", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// --- Index repo ---

func TestIndexRepo_NotFound(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/repos/nonexistent/index", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIndexRepo_Busy(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	_ = srv.repoStore.Create(&storage.Repo{
		Name: "busy", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusIndexing, LocalPath: "/tmp/busy",
	})

	resp := doRequest(t, "POST", ts.URL+"/api/repos/busy/index", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIndexRepo_Accepted(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	_ = srv.repoStore.Create(&storage.Repo{
		Name: "ready", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: t.TempDir(),
	})

	resp := doRequest(t, "POST", ts.URL+"/api/repos/ready/index", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected 202, got %d", resp.StatusCode)
	}

	var repo storage.Repo
	decodeJSON(t, resp, &repo)
	if repo.Name != "ready" {
		t.Errorf("expected name=ready, got %s", repo.Name)
	}

	// Give goroutine time to complete
	time.Sleep(100 * time.Millisecond)
}

// --- Search repo ---

func TestSearchRepo_EmptyQuery(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/repos/test/search", searchRequest{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSearchRepo_RepoNotFound(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/repos/nonexistent/search", searchRequest{Query: "test"})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSearchRepo_RepoNotIndexed(t *testing.T) {
	srv, ts := setupTestServer(t)
	defer ts.Close()

	_ = srv.repoStore.Create(&storage.Repo{
		Name: "test", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: "/tmp/test",
	})

	resp := doRequest(t, "POST", ts.URL+"/api/repos/test/search", searchRequest{Query: "hello"})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// --- Search all ---

func TestSearchAll_EmptyQuery(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/search", searchRequest{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSearchAll_NoIndexedRepos(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/search", searchRequest{Query: "test"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body searchResponse
	decodeJSON(t, resp, &body)
	if len(body.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(body.Results))
	}
}

// --- ast-grep ---

func TestAstSearchLanguages(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "GET", ts.URL+"/api/ast-search/languages", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body languagesResponse
	decodeJSON(t, resp, &body)
	if len(body.Languages) == 0 {
		t.Error("expected non-empty languages list")
	}
}

func TestAstSearch_EmptyPattern(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/repos/test/ast-search", astSearchRequest{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAstSearch_RepoNotFound(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	resp := doRequest(t, "POST", ts.URL+"/api/repos/nonexistent/ast-search", astSearchRequest{Pattern: "func $NAME()"})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
