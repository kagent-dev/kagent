package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/indexer"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/search"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPServer exposes gitrepo-mcp functionality via MCP protocol.
type MCPServer struct {
	repoStore   *storage.RepoStore
	indexer     *indexer.Indexer
	searcher    *search.Searcher
	astSearcher *search.AstSearcher
	reposDir    string
	server      *mcpsdk.Server
	httpHandler *mcpsdk.StreamableHTTPHandler
}

// --- Input/Output types ---

type AddRepoInput struct {
	Name   string `json:"name" jsonschema:"repository name (short identifier)"`
	URL    string `json:"url" jsonschema:"git clone URL"`
	Branch string `json:"branch,omitempty" jsonschema:"git branch (default: main)"`
}

type AddRepoOutput struct {
	Repo storage.Repo `json:"repo"`
}

type ListReposInput struct{}

type ListReposOutput struct {
	Repos []storage.Repo `json:"repos"`
}

type RemoveRepoInput struct {
	Name string `json:"name" jsonschema:"repository name to remove"`
}

type RemoveRepoOutput struct {
	Removed bool `json:"removed"`
}

type SyncRepoInput struct {
	Name string `json:"name" jsonschema:"repository name to sync"`
}

type SyncRepoOutput struct {
	Repo storage.Repo `json:"repo"`
}

type IndexRepoInput struct {
	Name string `json:"name" jsonschema:"repository name to index"`
}

type IndexRepoOutput struct {
	Repo storage.Repo `json:"repo"`
}

type SearchCodeInput struct {
	Query        string `json:"query" jsonschema:"semantic search query"`
	Repo         string `json:"repo,omitempty" jsonschema:"repository name (omit to search all indexed repos)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max results (default 10)"`
	ContextLines int    `json:"contextLines,omitempty" jsonschema:"context lines before/after each match"`
}

type SearchCodeOutput struct {
	Results []search.SearchResult `json:"results"`
}

type AstSearchInput struct {
	Pattern  string `json:"pattern" jsonschema:"ast-grep pattern (e.g. func $NAME($$$) error)"`
	Repo     string `json:"repo" jsonschema:"repository name"`
	Language string `json:"language,omitempty" jsonschema:"language filter (e.g. go, python)"`
}

type AstSearchOutput struct {
	Results []search.AstSearchResult `json:"results"`
}

type AstSearchLanguagesInput struct{}

type AstSearchLanguagesOutput struct {
	Languages []string `json:"languages"`
}

// NewMCPServer creates an MCP server with all tools registered.
func NewMCPServer(
	repoStore *storage.RepoStore,
	idx *indexer.Indexer,
	searcher *search.Searcher,
	astSearcher *search.AstSearcher,
	reposDir string,
) *MCPServer {
	m := &MCPServer{
		repoStore:   repoStore,
		indexer:     idx,
		searcher:    searcher,
		astSearcher: astSearcher,
		reposDir:    reposDir,
	}

	impl := &mcpsdk.Implementation{
		Name:    "gitrepo-mcp",
		Version: "0.1.0",
	}
	srv := mcpsdk.NewServer(impl, nil)
	m.server = srv

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "add_repo",
		Description: "Register and clone a git repository. Returns immediately with status 'cloning'; poll with list_repos to check when clone completes.",
	}, m.handleAddRepo)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_repos",
		Description: "List all registered git repositories with their status, file count, and chunk count.",
	}, m.handleListRepos)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "remove_repo",
		Description: "Remove a git repository and all its indexed data.",
	}, m.handleRemoveRepo)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "sync_repo",
		Description: "Pull latest changes for a repository (git pull --ff-only).",
	}, m.handleSyncRepo)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "index_repo",
		Description: "Index a repository for semantic search. Starts indexing in the background; poll with list_repos to check when indexing completes.",
	}, m.handleIndexRepo)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "search_code",
		Description: "Semantic code search across indexed repositories. Returns ranked results with file paths, line ranges, scores, and optional context lines.",
	}, m.handleSearchCode)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "ast_search",
		Description: "Structural code search using ast-grep patterns (e.g. 'func $NAME($$$) error'). Requires ast-grep binary in PATH.",
	}, m.handleAstSearch)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "ast_search_languages",
		Description: "List programming languages supported by ast-grep structural search.",
	}, m.handleAstSearchLanguages)

	m.httpHandler = mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return srv },
		nil,
	)

	return m
}

// Server returns the underlying MCP server (for stdio transport).
func (m *MCPServer) Server() *mcpsdk.Server {
	return m.server
}

// ServeHTTP implements http.Handler for the StreamableHTTP transport.
func (m *MCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.httpHandler.ServeHTTP(w, r)
}

// --- Tool handlers ---

func (m *MCPServer) handleAddRepo(_ context.Context, _ *mcpsdk.CallToolRequest, in AddRepoInput) (*mcpsdk.CallToolResult, AddRepoOutput, error) {
	if in.Name == "" {
		return mcpError("name is required"), AddRepoOutput{}, nil
	}
	if in.URL == "" {
		return mcpError("url is required"), AddRepoOutput{}, nil
	}
	if in.Branch == "" {
		in.Branch = "main"
	}

	if existing, _ := m.repoStore.Get(in.Name); existing != nil {
		return mcpError("repo %s already exists", in.Name), AddRepoOutput{}, nil
	}

	localPath := filepath.Join(m.reposDir, in.Name)
	repo := &storage.Repo{
		Name:      in.Name,
		URL:       in.URL,
		Branch:    in.Branch,
		Status:    storage.RepoStatusCloning,
		LocalPath: localPath,
	}

	if err := m.repoStore.Create(repo); err != nil {
		return mcpError("failed to create repo: %v", err), AddRepoOutput{}, nil
	}

	go m.cloneBackground(in.Name, in.URL, in.Branch, localPath)

	out := AddRepoOutput{Repo: *repo}
	return mcpText("Repo %s registered (status: cloning). Clone started in background.", in.Name), out, nil
}

func (m *MCPServer) handleListRepos(_ context.Context, _ *mcpsdk.CallToolRequest, _ ListReposInput) (*mcpsdk.CallToolResult, ListReposOutput, error) {
	repos, err := m.repoStore.List()
	if err != nil {
		return mcpError("failed to list repos: %v", err), ListReposOutput{}, nil
	}

	out := ListReposOutput{Repos: repos}

	var sb strings.Builder
	if len(repos) == 0 {
		sb.WriteString("No repositories registered.")
	} else {
		for i, r := range repos {
			if i > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(fmt.Sprintf("%s [%s] %s (branch: %s, files: %d, chunks: %d)",
				r.Name, r.Status, r.URL, r.Branch, r.FileCount, r.ChunkCount))
		}
	}

	return mcpText("%s", sb.String()), out, nil
}

func (m *MCPServer) handleRemoveRepo(_ context.Context, _ *mcpsdk.CallToolRequest, in RemoveRepoInput) (*mcpsdk.CallToolResult, RemoveRepoOutput, error) {
	if in.Name == "" {
		return mcpError("name is required"), RemoveRepoOutput{}, nil
	}

	repo, err := m.repoStore.Get(in.Name)
	if err != nil {
		return mcpError("repo %s not found", in.Name), RemoveRepoOutput{}, nil
	}

	if repo.LocalPath != "" {
		_ = os.RemoveAll(repo.LocalPath)
	}

	if err := m.repoStore.Delete(in.Name); err != nil {
		return mcpError("failed to delete repo: %v", err), RemoveRepoOutput{}, nil
	}

	return mcpText("Repo %s removed.", in.Name), RemoveRepoOutput{Removed: true}, nil
}

func (m *MCPServer) handleSyncRepo(_ context.Context, _ *mcpsdk.CallToolRequest, in SyncRepoInput) (*mcpsdk.CallToolResult, SyncRepoOutput, error) {
	if in.Name == "" {
		return mcpError("name is required"), SyncRepoOutput{}, nil
	}

	repo, err := m.repoStore.Get(in.Name)
	if err != nil {
		return mcpError("repo %s not found", in.Name), SyncRepoOutput{}, nil
	}

	if repo.Status == storage.RepoStatusCloning || repo.Status == storage.RepoStatusIndexing {
		return mcpError("repo %s is busy (status: %s)", in.Name, repo.Status), SyncRepoOutput{}, nil
	}

	cmd := exec.Command("git", "-C", repo.LocalPath, "pull", "--ff-only")
	if err := cmd.Run(); err != nil {
		errMsg := fmt.Sprintf("git pull failed: %v", err)
		repo.Status = storage.RepoStatusError
		repo.Error = &errMsg
		_ = m.repoStore.Update(repo)
		return mcpError("%s", errMsg), SyncRepoOutput{}, nil
	}

	now := time.Now()
	repo.LastSynced = &now
	repo.Error = nil
	if repo.Status == storage.RepoStatusError {
		repo.Status = storage.RepoStatusCloned
	}
	if err := m.repoStore.Update(repo); err != nil {
		return mcpError("failed to update repo: %v", err), SyncRepoOutput{}, nil
	}

	return mcpText("Repo %s synced.", in.Name), SyncRepoOutput{Repo: *repo}, nil
}

func (m *MCPServer) handleIndexRepo(_ context.Context, _ *mcpsdk.CallToolRequest, in IndexRepoInput) (*mcpsdk.CallToolResult, IndexRepoOutput, error) {
	if in.Name == "" {
		return mcpError("name is required"), IndexRepoOutput{}, nil
	}

	repo, err := m.repoStore.Get(in.Name)
	if err != nil {
		return mcpError("repo %s not found", in.Name), IndexRepoOutput{}, nil
	}

	if repo.Status == storage.RepoStatusCloning || repo.Status == storage.RepoStatusIndexing {
		return mcpError("repo %s is busy (status: %s)", in.Name, repo.Status), IndexRepoOutput{}, nil
	}

	go func() {
		if err := m.indexer.Index(in.Name); err != nil {
			log.Printf("background indexing of repo %s failed: %v", in.Name, err)
		}
	}()

	return mcpText("Indexing started for repo %s.", in.Name), IndexRepoOutput{Repo: *repo}, nil
}

func (m *MCPServer) handleSearchCode(_ context.Context, _ *mcpsdk.CallToolRequest, in SearchCodeInput) (*mcpsdk.CallToolResult, SearchCodeOutput, error) {
	if in.Query == "" {
		return mcpError("query is required"), SearchCodeOutput{}, nil
	}
	if in.Limit <= 0 {
		in.Limit = 10
	}

	var allResults []search.SearchResult

	if in.Repo != "" {
		results, err := m.searcher.Search(in.Query, in.Repo, in.Limit, in.ContextLines)
		if err != nil {
			return mcpError("%s", err), SearchCodeOutput{}, nil
		}
		allResults = results
	} else {
		repos, err := m.repoStore.List()
		if err != nil {
			return mcpError("failed to list repos: %v", err), SearchCodeOutput{}, nil
		}

		for _, repo := range repos {
			if repo.Status != storage.RepoStatusIndexed {
				continue
			}
			results, err := m.searcher.Search(in.Query, repo.Name, 0, in.ContextLines)
			if err != nil {
				log.Printf("search in repo %s failed: %v", repo.Name, err)
				continue
			}
			allResults = append(allResults, results...)
		}

		sort.Slice(allResults, func(i, j int) bool {
			return allResults[i].Score > allResults[j].Score
		})
		if len(allResults) > in.Limit {
			allResults = allResults[:in.Limit]
		}
	}

	if allResults == nil {
		allResults = []search.SearchResult{}
	}

	out := SearchCodeOutput{Results: allResults}

	var sb strings.Builder
	if len(allResults) == 0 {
		sb.WriteString("No results found.")
	} else {
		for i, r := range allResults {
			if i > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(fmt.Sprintf("[%.4f] %s:%s:%d-%d (%s)",
				r.Score, r.Repo, r.FilePath, r.LineStart, r.LineEnd, r.ChunkType))
			if r.ChunkName != "" {
				sb.WriteString(fmt.Sprintf(" %s", r.ChunkName))
			}
			sb.WriteByte('\n')
			sb.WriteString(r.Content)
		}
	}

	return mcpText("%s", sb.String()), out, nil
}

func (m *MCPServer) handleAstSearch(_ context.Context, _ *mcpsdk.CallToolRequest, in AstSearchInput) (*mcpsdk.CallToolResult, AstSearchOutput, error) {
	if in.Pattern == "" {
		return mcpError("pattern is required"), AstSearchOutput{}, nil
	}
	if in.Repo == "" {
		return mcpError("repo is required"), AstSearchOutput{}, nil
	}

	results, err := m.astSearcher.Search(in.Pattern, in.Repo, in.Language)
	if err != nil {
		return mcpError("%s", err), AstSearchOutput{}, nil
	}

	if results == nil {
		results = []search.AstSearchResult{}
	}

	out := AstSearchOutput{Results: results}

	var sb strings.Builder
	if len(results) == 0 {
		sb.WriteString("No matches found.")
	} else {
		for i, r := range results {
			if i > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(fmt.Sprintf("%s:%d-%d [%s]\n%s", r.FilePath, r.LineStart, r.LineEnd, r.Language, r.Content))
		}
	}

	return mcpText("%s", sb.String()), out, nil
}

func (m *MCPServer) handleAstSearchLanguages(_ context.Context, _ *mcpsdk.CallToolRequest, _ AstSearchLanguagesInput) (*mcpsdk.CallToolResult, AstSearchLanguagesOutput, error) {
	langs := search.SupportedLanguages()
	out := AstSearchLanguagesOutput{Languages: langs}
	return mcpText("Supported languages: %s", strings.Join(langs, ", ")), out, nil
}

// --- Background operations ---

func (m *MCPServer) cloneBackground(name, url, branch, localPath string) {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		log.Printf("failed to create parent directory for %s: %v", name, err)
		m.setRepoError(name, err)
		return
	}

	cmd := exec.Command("git", "clone",
		"--branch", branch,
		"--single-branch",
		"--depth", "1",
		url, localPath,
	)
	if err := cmd.Run(); err != nil {
		log.Printf("background clone of repo %s failed: %v", name, err)
		m.setRepoError(name, err)
		return
	}

	repo, err := m.repoStore.Get(name)
	if err != nil {
		log.Printf("failed to get repo %s after clone: %v", name, err)
		return
	}
	now := time.Now()
	repo.Status = storage.RepoStatusCloned
	repo.LastSynced = &now
	repo.Error = nil
	if err := m.repoStore.Update(repo); err != nil {
		log.Printf("failed to update repo %s after clone: %v", name, err)
	}
}

func (m *MCPServer) setRepoError(name string, opErr error) {
	repo, err := m.repoStore.Get(name)
	if err != nil {
		return
	}
	errMsg := opErr.Error()
	repo.Status = storage.RepoStatusError
	repo.Error = &errMsg
	_ = m.repoStore.Update(repo)
}

// --- Helpers ---

func mcpError(format string, args ...interface{}) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: fmt.Sprintf(format, args...)},
		},
		IsError: true,
	}
}

func mcpText(format string, args ...interface{}) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: fmt.Sprintf(format, args...)},
		},
	}
}

