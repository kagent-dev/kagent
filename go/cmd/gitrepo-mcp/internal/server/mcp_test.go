package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/config"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/embedder"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/indexer"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/search"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// setupMCPTest creates an MCP server and connects a client session via in-memory transport.
func setupMCPTest(t *testing.T) (*MCPServer, *mcpsdk.ClientSession) {
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

	mcpSrv := NewMCPServer(repoStore, idx, s, astS, reposDir)

	ctx := context.Background()
	t1, t2 := mcpsdk.NewInMemoryTransports()
	if _, err := mcpSrv.Server().Connect(ctx, t1, nil); err != nil {
		t.Fatal(err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { session.Close() })

	return mcpSrv, session
}

// callTool is a helper to call an MCP tool and return the result.
func callTool(t *testing.T, session *mcpsdk.ClientSession, name string, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s) failed: %v", name, err)
	}
	return result
}

// callToolExpectError calls an MCP tool and expects an error (e.g., schema validation).
func callToolExpectError(t *testing.T, session *mcpsdk.ClientSession, name string, args map[string]any) {
	t.Helper()
	_, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err == nil {
		t.Errorf("CallTool(%s) expected error, got nil", name)
	}
}

// resultText extracts the text content from a CallToolResult.
func resultText(t *testing.T, result *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		return ""
	}
	tc, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

// --- Tool registration ---

func TestMCP_ToolsRegistered(t *testing.T) {
	_, session := setupMCPTest(t)

	tools := map[string]bool{}
	for tool, err := range session.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools[tool.Name] = true
	}

	expected := []string{
		"add_repo", "list_repos", "remove_repo", "sync_repo",
		"index_repo", "search_code", "ast_search", "ast_search_languages",
	}
	for _, name := range expected {
		if !tools[name] {
			t.Errorf("expected tool %s to be registered", name)
		}
	}
	if len(tools) != len(expected) {
		t.Errorf("expected %d tools, got %d", len(expected), len(tools))
	}
}

// --- list_repos ---

func TestMCP_ListRepos_Empty(t *testing.T) {
	_, session := setupMCPTest(t)

	result := callTool(t, session, "list_repos", nil)
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(t, result))
	}
	text := resultText(t, result)
	if text != "No repositories registered." {
		t.Errorf("unexpected text: %s", text)
	}
}

func TestMCP_ListRepos_WithRepos(t *testing.T) {
	mcpSrv, session := setupMCPTest(t)

	_ = mcpSrv.repoStore.Create(&storage.Repo{
		Name: "alpha", URL: "http://example.com/alpha.git", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: "/tmp/alpha",
	})
	_ = mcpSrv.repoStore.Create(&storage.Repo{
		Name: "beta", URL: "http://example.com/beta.git", Branch: "dev",
		Status: storage.RepoStatusIndexed, LocalPath: "/tmp/beta", FileCount: 10, ChunkCount: 50,
	})

	result := callTool(t, session, "list_repos", nil)
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(t, result))
	}

	// Check structured output
	if result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("failed to marshal structured content: %v", err)
		}
		var out ListReposOutput
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("failed to unmarshal structured content: %v", err)
		}
		if len(out.Repos) != 2 {
			t.Errorf("expected 2 repos in structured output, got %d", len(out.Repos))
		}
	}
}

// --- add_repo ---

func TestMCP_AddRepo_MissingName(t *testing.T) {
	_, session := setupMCPTest(t)
	// SDK validates schema: name is required
	callToolExpectError(t, session, "add_repo", map[string]any{
		"url": "http://example.com/repo.git",
	})
}

func TestMCP_AddRepo_MissingURL(t *testing.T) {
	_, session := setupMCPTest(t)
	// SDK validates schema: url is required
	callToolExpectError(t, session, "add_repo", map[string]any{
		"name": "test",
	})
}

func TestMCP_AddRepo_Success(t *testing.T) {
	_, session := setupMCPTest(t)

	result := callTool(t, session, "add_repo", map[string]any{
		"name": "test",
		"url":  "http://invalid-url/repo.git",
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(t, result))
	}

	text := resultText(t, result)
	if text == "" {
		t.Error("expected non-empty response")
	}
}

func TestMCP_AddRepo_DefaultBranch(t *testing.T) {
	_, session := setupMCPTest(t)

	result := callTool(t, session, "add_repo", map[string]any{
		"name": "test",
		"url":  "http://invalid-url/repo.git",
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(t, result))
	}

	// Verify via list_repos
	listResult := callTool(t, session, "list_repos", nil)
	text := resultText(t, listResult)
	if text == "" || text == "No repositories registered." {
		t.Error("expected repos after add")
	}
}

func TestMCP_AddRepo_Duplicate(t *testing.T) {
	mcpSrv, session := setupMCPTest(t)

	_ = mcpSrv.repoStore.Create(&storage.Repo{
		Name: "test", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: "/tmp/test",
	})

	result := callTool(t, session, "add_repo", map[string]any{
		"name": "test",
		"url":  "http://example.com/repo.git",
	})
	if !result.IsError {
		t.Error("expected error for duplicate repo")
	}
}

// --- remove_repo ---

func TestMCP_RemoveRepo_Success(t *testing.T) {
	mcpSrv, session := setupMCPTest(t)

	_ = mcpSrv.repoStore.Create(&storage.Repo{
		Name: "test", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: t.TempDir(),
	})

	result := callTool(t, session, "remove_repo", map[string]any{"name": "test"})
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(t, result))
	}

	// Verify removed
	listResult := callTool(t, session, "list_repos", nil)
	text := resultText(t, listResult)
	if text != "No repositories registered." {
		t.Errorf("expected empty list after remove, got: %s", text)
	}
}

func TestMCP_RemoveRepo_NotFound(t *testing.T) {
	_, session := setupMCPTest(t)

	result := callTool(t, session, "remove_repo", map[string]any{"name": "nonexistent"})
	if !result.IsError {
		t.Error("expected error for nonexistent repo")
	}
}

func TestMCP_RemoveRepo_MissingName(t *testing.T) {
	_, session := setupMCPTest(t)
	// SDK validates schema: name is required
	callToolExpectError(t, session, "remove_repo", map[string]any{})
}

// --- sync_repo ---

func TestMCP_SyncRepo_NotFound(t *testing.T) {
	_, session := setupMCPTest(t)

	result := callTool(t, session, "sync_repo", map[string]any{"name": "nonexistent"})
	if !result.IsError {
		t.Error("expected error for nonexistent repo")
	}
}

func TestMCP_SyncRepo_Busy(t *testing.T) {
	mcpSrv, session := setupMCPTest(t)

	_ = mcpSrv.repoStore.Create(&storage.Repo{
		Name: "busy", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusCloning, LocalPath: "/tmp/busy",
	})

	result := callTool(t, session, "sync_repo", map[string]any{"name": "busy"})
	if !result.IsError {
		t.Error("expected error for busy repo")
	}
}

// --- index_repo ---

func TestMCP_IndexRepo_NotFound(t *testing.T) {
	_, session := setupMCPTest(t)

	result := callTool(t, session, "index_repo", map[string]any{"name": "nonexistent"})
	if !result.IsError {
		t.Error("expected error for nonexistent repo")
	}
}

func TestMCP_IndexRepo_Busy(t *testing.T) {
	mcpSrv, session := setupMCPTest(t)

	_ = mcpSrv.repoStore.Create(&storage.Repo{
		Name: "busy", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusIndexing, LocalPath: "/tmp/busy",
	})

	result := callTool(t, session, "index_repo", map[string]any{"name": "busy"})
	if !result.IsError {
		t.Error("expected error for busy repo")
	}
}

func TestMCP_IndexRepo_Success(t *testing.T) {
	mcpSrv, session := setupMCPTest(t)

	_ = mcpSrv.repoStore.Create(&storage.Repo{
		Name: "ready", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: t.TempDir(),
	})

	result := callTool(t, session, "index_repo", map[string]any{"name": "ready"})
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(t, result))
	}
}

// --- search_code ---

func TestMCP_SearchCode_EmptyQuery(t *testing.T) {
	_, session := setupMCPTest(t)
	// SDK validates schema: query is required
	callToolExpectError(t, session, "search_code", map[string]any{})
}

func TestMCP_SearchCode_RepoNotFound(t *testing.T) {
	_, session := setupMCPTest(t)

	result := callTool(t, session, "search_code", map[string]any{
		"query": "test",
		"repo":  "nonexistent",
	})
	if !result.IsError {
		t.Error("expected error for nonexistent repo")
	}
}

func TestMCP_SearchCode_RepoNotIndexed(t *testing.T) {
	mcpSrv, session := setupMCPTest(t)

	_ = mcpSrv.repoStore.Create(&storage.Repo{
		Name: "test", URL: "http://example.com", Branch: "main",
		Status: storage.RepoStatusCloned, LocalPath: "/tmp/test",
	})

	result := callTool(t, session, "search_code", map[string]any{
		"query": "hello",
		"repo":  "test",
	})
	if !result.IsError {
		t.Error("expected error for non-indexed repo")
	}
}

func TestMCP_SearchCode_NoIndexedRepos(t *testing.T) {
	_, session := setupMCPTest(t)

	result := callTool(t, session, "search_code", map[string]any{
		"query": "test",
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(t, result))
	}
	text := resultText(t, result)
	if text != "No results found." {
		t.Errorf("expected 'No results found.', got: %s", text)
	}
}

// --- ast_search ---

func TestMCP_AstSearch_EmptyPattern(t *testing.T) {
	_, session := setupMCPTest(t)
	// SDK validates schema: pattern is required
	callToolExpectError(t, session, "ast_search", map[string]any{
		"repo": "test",
	})
}

func TestMCP_AstSearch_MissingRepo(t *testing.T) {
	_, session := setupMCPTest(t)
	// SDK validates schema: repo is required
	callToolExpectError(t, session, "ast_search", map[string]any{
		"pattern": "func $NAME()",
	})
}

func TestMCP_AstSearch_RepoNotFound(t *testing.T) {
	_, session := setupMCPTest(t)

	result := callTool(t, session, "ast_search", map[string]any{
		"pattern": "func $NAME()",
		"repo":    "nonexistent",
	})
	if !result.IsError {
		t.Error("expected error for nonexistent repo")
	}
}

// --- ast_search_languages ---

func TestMCP_AstSearchLanguages(t *testing.T) {
	_, session := setupMCPTest(t)

	result := callTool(t, session, "ast_search_languages", nil)
	if result.IsError {
		t.Errorf("unexpected error: %s", resultText(t, result))
	}

	text := resultText(t, result)
	if text == "" {
		t.Error("expected non-empty languages list")
	}

	// Check structured output has languages
	if result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("failed to marshal structured content: %v", err)
		}
		var out AstSearchLanguagesOutput
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("failed to unmarshal structured content: %v", err)
		}
		if len(out.Languages) == 0 {
			t.Error("expected non-empty languages in structured output")
		}
	}
}
