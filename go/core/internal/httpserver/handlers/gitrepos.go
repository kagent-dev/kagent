package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// GitReposHandler proxies git repository management requests to the gitrepo-mcp service.
type GitReposHandler struct {
	*Base
	GitRepoMCPURL string
	httpClient    *http.Client
}

// NewGitReposHandler creates a new GitReposHandler.
func NewGitReposHandler(base *Base, gitRepoMCPURL string) *GitReposHandler {
	return &GitReposHandler{
		Base:          base,
		GitRepoMCPURL: gitRepoMCPURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// proxy forwards a request to the gitrepo-mcp service and streams the response back.
func (h *GitReposHandler) proxy(w ErrorResponseWriter, r *http.Request, method, downstreamPath string) {
	log := ctrllog.FromContext(r.Context()).WithName("gitrepos-handler")

	if h.GitRepoMCPURL == "" {
		w.RespondWithError(errors.NewServiceUnavailableError("gitrepo-mcp service not configured", nil))
		return
	}

	targetURL := strings.TrimRight(h.GitRepoMCPURL, "/") + downstreamPath

	req, err := http.NewRequestWithContext(r.Context(), method, targetURL, r.Body)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("failed to create proxy request", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	log.V(1).Info("Proxying request", "method", method, "target", targetURL)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Error(err, "Failed to reach gitrepo-mcp service", "url", targetURL)
		w.RespondWithError(errors.NewBadGatewayError("gitrepo-mcp service unavailable", err))
		return
	}
	defer resp.Body.Close()

	// Copy headers from downstream response
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// HandleListRepos handles GET /api/gitrepos
func (h *GitReposHandler) HandleListRepos(w ErrorResponseWriter, r *http.Request) {
	if err := Check(h.Authorizer, r, auth.Resource{Type: "GitRepo"}); err != nil {
		w.RespondWithError(err)
		return
	}
	h.proxy(w, r, http.MethodGet, "/api/repos")
}

// HandleAddRepo handles POST /api/gitrepos
func (h *GitReposHandler) HandleAddRepo(w ErrorResponseWriter, r *http.Request) {
	if err := Check(h.Authorizer, r, auth.Resource{Type: "GitRepo"}); err != nil {
		w.RespondWithError(err)
		return
	}
	h.proxy(w, r, http.MethodPost, "/api/repos")
}

// HandleGetRepo handles GET /api/gitrepos/{name}
func (h *GitReposHandler) HandleGetRepo(w ErrorResponseWriter, r *http.Request) {
	if err := Check(h.Authorizer, r, auth.Resource{Type: "GitRepo"}); err != nil {
		w.RespondWithError(err)
		return
	}
	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("name is required", err))
		return
	}
	h.proxy(w, r, http.MethodGet, fmt.Sprintf("/api/repos/%s", name))
}

// HandleDeleteRepo handles DELETE /api/gitrepos/{name}
func (h *GitReposHandler) HandleDeleteRepo(w ErrorResponseWriter, r *http.Request) {
	if err := Check(h.Authorizer, r, auth.Resource{Type: "GitRepo"}); err != nil {
		w.RespondWithError(err)
		return
	}
	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("name is required", err))
		return
	}
	h.proxy(w, r, http.MethodDelete, fmt.Sprintf("/api/repos/%s", name))
}

// HandleSyncRepo handles POST /api/gitrepos/{name}/sync
func (h *GitReposHandler) HandleSyncRepo(w ErrorResponseWriter, r *http.Request) {
	if err := Check(h.Authorizer, r, auth.Resource{Type: "GitRepo"}); err != nil {
		w.RespondWithError(err)
		return
	}
	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("name is required", err))
		return
	}
	h.proxy(w, r, http.MethodPost, fmt.Sprintf("/api/repos/%s/sync", name))
}

// HandleIndexRepo handles POST /api/gitrepos/{name}/index
func (h *GitReposHandler) HandleIndexRepo(w ErrorResponseWriter, r *http.Request) {
	if err := Check(h.Authorizer, r, auth.Resource{Type: "GitRepo"}); err != nil {
		w.RespondWithError(err)
		return
	}
	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("name is required", err))
		return
	}
	h.proxy(w, r, http.MethodPost, fmt.Sprintf("/api/repos/%s/index", name))
}

// HandleSearchRepo handles POST /api/gitrepos/{name}/search
func (h *GitReposHandler) HandleSearchRepo(w ErrorResponseWriter, r *http.Request) {
	if err := Check(h.Authorizer, r, auth.Resource{Type: "GitRepo"}); err != nil {
		w.RespondWithError(err)
		return
	}
	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("name is required", err))
		return
	}
	h.proxy(w, r, http.MethodPost, fmt.Sprintf("/api/repos/%s/search", name))
}

// HandleSearchAll handles POST /api/gitrepos/search
func (h *GitReposHandler) HandleSearchAll(w ErrorResponseWriter, r *http.Request) {
	if err := Check(h.Authorizer, r, auth.Resource{Type: "GitRepo"}); err != nil {
		w.RespondWithError(err)
		return
	}
	h.proxy(w, r, http.MethodPost, "/api/search")
}
