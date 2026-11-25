package skillsinit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

func TestCloneGitCommitRejectsMutableRef(t *testing.T) {
	err := CloneGitCommit(context.Background(), "https://example.com/repository.git", "main", t.TempDir())
	require.ErrorContains(t, err, "full SHA")
}

func TestCloneGitCommitFetchesSpecificCommit(t *testing.T) {
	src := t.TempDir()
	repo, err := git.PlainInit(src, false)
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	sig := &object.Signature{
		Name:  "Test",
		Email: "test@example.com",
		When:  time.Unix(1700000000, 0),
	}

	// First commit (the one we want to fetch)
	require.NoError(t, os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# v1"), 0o644))
	_, err = wt.Add("SKILL.md")
	require.NoError(t, err)
	firstCommit, err := wt.Commit("first", &git.CommitOptions{Author: sig, Committer: sig})
	require.NoError(t, err)

	// Second commit on HEAD (to prove we check out firstCommit, not HEAD)
	require.NoError(t, os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# v2"), 0o644))
	_, err = wt.Add("SKILL.md")
	require.NoError(t, err)
	_, err = wt.Commit("second", &git.CommitOptions{Author: sig, Committer: sig})
	require.NoError(t, err)

	dest := t.TempDir()
	err = CloneGitCommit(context.Background(), src, firstCommit.String(), dest)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "# v1", string(content))
}

func TestCloneGitCommitHonorsContextTimeout(t *testing.T) {
	unblock := make(chan struct{})

	// Simulate a git server that accepts the connection and goes silent (#3452)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-unblock:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(unblock)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := CloneGitCommit(ctx, srv.URL+"/repo.git", strings.Repeat("a", 40), t.TempDir())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
