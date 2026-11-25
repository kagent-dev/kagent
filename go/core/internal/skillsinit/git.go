package skillsinit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

func init() {
	// Replace go-git's default file:// transport (which shells out to git-upload-pack)
	// with the pure-Go in-process filesystem server.
	client.InstallProtocol("file", server.NewClient(server.NewFilesystemLoader(osfs.New("/"))))
}

var immutableGitCommit = regexp.MustCompile(`^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// CloneGitCommit fetches only one immutable commit instead of cloning the
// repository's complete history.
func CloneGitCommit(ctx context.Context, url, commit, destination string) error {
	if !immutableGitCommit.MatchString(commit) {
		return fmt.Errorf("git commit must be a full SHA")
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}

	// If url points to a local non-bare repository directory, point go-git's
	// in-process file loader at its .git directory.
	if info, err := os.Stat(filepath.Join(url, ".git")); err == nil && info.IsDir() {
		url = filepath.Join(url, ".git")
	}

	repo, err := git.PlainInit(destination, false)
	if err != nil {
		return fmt.Errorf("git init: %w", err)
	}

	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	}); err != nil {
		return fmt.Errorf("git remote add: %w", err)
	}

	refSpec := config.RefSpec(fmt.Sprintf("%s:refs/remotes/origin/%s", commit, commit))

	// Try a shallow fetch of the specific commit first.
	err = repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refSpec},
		Depth:      1,
		Tags:       git.NoTags,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Fallback for transports (like the in-process local filesystem loader)
		// or servers that do not support shallow fetches by arbitrary SHA.
		err = repo.FetchContext(ctx, &git.FetchOptions{
			RemoteName: "origin",
			Tags:       git.AllTags,
		})
		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("git fetch: %w", err)
		}
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("git worktree: %w", err)
	}

	return wt.Checkout(&git.CheckoutOptions{
		Hash:  plumbing.NewHash(commit),
		Force: true,
	})
}
