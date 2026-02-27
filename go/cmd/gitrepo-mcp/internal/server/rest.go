package server

import (
	"encoding/json"
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
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/repo"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/search"
	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
)

// Server serves the REST API for gitrepo-mcp.
type Server struct {
	repoStore   *storage.RepoStore
	repoManager *repo.Manager
	indexer     *indexer.Indexer
	searcher    *search.Searcher
	astSearcher *search.AstSearcher
	reposDir    string
}

// NewServer creates a REST API server.
func NewServer(
	repoStore *storage.RepoStore,
	repoManager *repo.Manager,
	idx *indexer.Indexer,
	searcher *search.Searcher,
	astSearcher *search.AstSearcher,
	reposDir string,
) *Server {
	return &Server{
		repoStore:   repoStore,
		repoManager: repoManager,
		indexer:     idx,
		searcher:    searcher,
		astSearcher: astSearcher,
		reposDir:    reposDir,
	}
}

// Handler returns the HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", s.handleHealth)

	// Repo CRUD
	mux.HandleFunc("GET /api/repos", s.handleListRepos)
	mux.HandleFunc("POST /api/repos", s.handleAddRepo)
	mux.HandleFunc("GET /api/repos/{name}", s.handleGetRepo)
	mux.HandleFunc("DELETE /api/repos/{name}", s.handleDeleteRepo)

	// Operations
	mux.HandleFunc("POST /api/repos/{name}/sync", s.handleSyncRepo)
	mux.HandleFunc("POST /api/repos/{name}/index", s.handleIndexRepo)
	mux.HandleFunc("POST /api/sync-all", s.handleSyncAll)

	// Search
	mux.HandleFunc("POST /api/repos/{name}/search", s.handleSearchRepo)
	mux.HandleFunc("POST /api/search", s.handleSearchAll)

	// ast-grep structural search
	mux.HandleFunc("POST /api/repos/{name}/ast-search", s.handleAstSearch)
	mux.HandleFunc("GET /api/ast-search/languages", s.handleAstSearchLanguages)

	return withLogging(mux)
}

// --- Request/Response types ---

type addRepoRequest struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Branch string `json:"branch,omitempty"`
}

type searchRequest struct {
	Query        string `json:"query"`
	Limit        int    `json:"limit,omitempty"`
	ContextLines int    `json:"contextLines,omitempty"`
}

type astSearchRequest struct {
	Pattern  string `json:"pattern"`
	Language string `json:"language,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type listReposResponse struct {
	Repos []storage.Repo `json:"repos"`
}

type searchResponse struct {
	Results []search.SearchResult `json:"results"`
}

type astSearchResponse struct {
	Results []search.AstSearchResult `json:"results"`
}

type languagesResponse struct {
	Languages []string `json:"languages"`
}

type syncAllResponse struct {
	Results []repo.SyncResult `json:"results"`
}

// --- Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListRepos(w http.ResponseWriter, _ *http.Request) {
	repos, err := s.repoStore.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repos: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, listReposResponse{Repos: repos})
}

func (s *Server) handleAddRepo(w http.ResponseWriter, r *http.Request) {
	var req addRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: %v", err)
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if req.Branch == "" {
		req.Branch = "main"
	}

	if existing, _ := s.repoStore.Get(req.Name); existing != nil {
		writeError(w, http.StatusConflict, "repo %s already exists", req.Name)
		return
	}

	localPath := filepath.Join(s.reposDir, req.Name)
	repoEntry := &storage.Repo{
		Name:      req.Name,
		URL:       req.URL,
		Branch:    req.Branch,
		Status:    storage.RepoStatusCloning,
		LocalPath: localPath,
	}

	if err := s.repoStore.Create(repoEntry); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create repo: %v", err)
		return
	}

	go s.cloneRepoBackground(req.Name, req.URL, req.Branch, localPath)

	writeJSON(w, http.StatusAccepted, repoEntry)
}

func (s *Server) handleGetRepo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	repo, err := s.repoStore.Get(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo %s not found", name)
		return
	}

	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) handleDeleteRepo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	repo, err := s.repoStore.Get(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo %s not found", name)
		return
	}

	if repo.LocalPath != "" {
		_ = os.RemoveAll(repo.LocalPath)
	}

	if err := s.repoStore.Delete(name); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete repo: %v", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSyncRepo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	syncedRepo, err := s.repoManager.Sync(name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "repo %s not found", name)
			return
		}
		if strings.Contains(err.Error(), "busy") {
			writeError(w, http.StatusConflict, "%s", err)
			return
		}
		writeError(w, http.StatusInternalServerError, "%s", err)
		return
	}

	// Trigger background re-index if repo was indexed
	if syncedRepo.Status == storage.RepoStatusIndexed {
		go func() {
			if err := s.indexer.Index(name); err != nil {
				log.Printf("background re-index of repo %s failed: %v", name, err)
			}
		}()
	}

	writeJSON(w, http.StatusOK, syncedRepo)
}

func (s *Server) handleSyncAll(w http.ResponseWriter, _ *http.Request) {
	reindexFn := func(name string) error {
		go func() {
			if err := s.indexer.Index(name); err != nil {
				log.Printf("background re-index of repo %s failed: %v", name, err)
			}
		}()
		return nil
	}

	results, err := s.repoManager.SyncAll(reindexFn)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sync repos: %v", err)
		return
	}
	if results == nil {
		results = []repo.SyncResult{}
	}

	writeJSON(w, http.StatusOK, syncAllResponse{Results: results})
}

func (s *Server) handleIndexRepo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	repo, err := s.repoStore.Get(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo %s not found", name)
		return
	}

	if repo.Status == storage.RepoStatusCloning || repo.Status == storage.RepoStatusIndexing {
		writeError(w, http.StatusConflict, "repo %s is busy (status: %s)", name, repo.Status)
		return
	}

	go func() {
		if err := s.indexer.Index(name); err != nil {
			log.Printf("background indexing of repo %s failed: %v", name, err)
		}
	}()

	writeJSON(w, http.StatusAccepted, repo)
}

func (s *Server) handleSearchRepo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: %v", err)
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	results, err := s.searcher.Search(req.Query, name, req.Limit, req.ContextLines)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "%s", err)
			return
		}
		if strings.Contains(err.Error(), "not indexed") {
			writeError(w, http.StatusConflict, "%s", err)
			return
		}
		writeError(w, http.StatusInternalServerError, "search failed: %v", err)
		return
	}

	if results == nil {
		results = []search.SearchResult{}
	}

	writeJSON(w, http.StatusOK, searchResponse{Results: results})
}

func (s *Server) handleSearchAll(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: %v", err)
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	repos, err := s.repoStore.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list repos: %v", err)
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	var allResults []search.SearchResult
	for _, repo := range repos {
		if repo.Status != storage.RepoStatusIndexed {
			continue
		}
		results, err := s.searcher.Search(req.Query, repo.Name, 0, req.ContextLines)
		if err != nil {
			log.Printf("search in repo %s failed: %v", repo.Name, err)
			continue
		}
		allResults = append(allResults, results...)
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})

	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	if allResults == nil {
		allResults = []search.SearchResult{}
	}

	writeJSON(w, http.StatusOK, searchResponse{Results: allResults})
}

func (s *Server) handleAstSearch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var req astSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: %v", err)
		return
	}

	if req.Pattern == "" {
		writeError(w, http.StatusBadRequest, "pattern is required")
		return
	}

	results, err := s.astSearcher.Search(req.Pattern, name, req.Language)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "%s", err)
			return
		}
		if strings.Contains(err.Error(), "not ready") {
			writeError(w, http.StatusConflict, "%s", err)
			return
		}
		writeError(w, http.StatusInternalServerError, "ast-search failed: %v", err)
		return
	}

	if results == nil {
		results = []search.AstSearchResult{}
	}

	writeJSON(w, http.StatusOK, astSearchResponse{Results: results})
}

func (s *Server) handleAstSearchLanguages(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, languagesResponse{Languages: search.SupportedLanguages()})
}

// --- Background operations ---

func (s *Server) cloneRepoBackground(name, url, branch, localPath string) {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		log.Printf("failed to create parent directory for %s: %v", name, err)
		s.setRepoError(name, err)
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
		s.setRepoError(name, err)
		return
	}

	repo, err := s.repoStore.Get(name)
	if err != nil {
		log.Printf("failed to get repo %s after clone: %v", name, err)
		return
	}
	now := time.Now()
	repo.Status = storage.RepoStatusCloned
	repo.LastSynced = &now
	repo.Error = nil
	if err := s.repoStore.Update(repo); err != nil {
		log.Printf("failed to update repo %s after clone: %v", name, err)
	}
}

func (s *Server) setRepoError(name string, opErr error) {
	repo, err := s.repoStore.Get(name)
	if err != nil {
		return
	}
	errMsg := opErr.Error()
	repo.Status = storage.RepoStatusError
	repo.Error = &errMsg
	_ = s.repoStore.Update(repo)
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	writeJSON(w, status, errorResponse{Error: msg})
}

// --- Middleware ---

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
