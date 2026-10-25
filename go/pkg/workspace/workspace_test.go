// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package workspace

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

type repositoryFixture struct {
	config   *configv1.GitWorkspaceConfig
	server   *httptest.Server
	requests *atomic.Int32
	commit   string
	root     string
}

// Use real Git's smart HTTPS protocol, including shallow fetch and commit refs.
func repository(t *testing.T, authorization bool, objectFormat string) repositoryFixture {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	require.NoError(t, os.Mkdir(work, 0o755))
	runGit(t, work, "init", "--initial-branch=main", "--object-format="+objectFormat)
	for _, name := range []string{"README.md", "AGENTS.md", "CLAUDE.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(work, name), []byte("initial repository instructions\n"), 0o644))
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-s", "-m", "initial")
	commit := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, root, "clone", "--bare", work, "repo.git")
	execPath := runGit(t, root, "--exec-path")
	backend := &cgi.Handler{Path: filepath.Join(execPath, "git-http-backend"), Dir: root, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	requests := &atomic.Int32{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if authorization && r.Header.Get("Authorization") != "kagent-credential-injected" {
			http.Error(w, "authorization required", http.StatusForbidden)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	certPath := filepath.Join(root, "ca.pem")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600))
	t.Setenv("GIT_SSL_CAINFO", certPath)
	return repositoryFixture{config: &configv1.GitWorkspaceConfig{Url: server.URL + "/repo.git", Ref: "main", Authorization: authorization}, server: server, requests: requests, commit: commit, root: root}
}

func runGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	args = append([]string{"-c", "user.name=Workspace Test", "-c", "user.email=workspace@example.com"}, args...)
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = directory
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return strings.TrimSpace(string(output))
}

func TestInitializeAndRestore(t *testing.T) {
	fixture := repository(t, true, "sha1")
	config, server, requests, commit := fixture.config, fixture.server, fixture.requests, fixture.commit
	seed := t.TempDir()
	require.NoError(t, Initialize(t.Context(), seed, config))
	require.Equal(t, commit, runGit(t, Path(seed), "rev-parse", "HEAD"))
	gitConfig, err := os.ReadFile(filepath.Join(Path(seed), ".git", "config"))
	require.NoError(t, err)
	require.NotContains(t, string(gitConfig), "extraHeader")
	require.NotContains(t, string(gitConfig), "credential")
	for _, file := range []string{"AGENTS.md", "CLAUDE.md"} {
		require.FileExists(t, filepath.Join(Path(seed), file))
	}
	before := requests.Load()
	require.Positive(t, before)
	server.Close()

	first, second := t.TempDir(), t.TempDir()
	require.NoError(t, os.CopyFS(first, os.DirFS(seed)))
	require.NoError(t, os.CopyFS(second, os.DirFS(seed)))
	require.NoError(t, Initialize(t.Context(), first, config))
	require.NoError(t, Initialize(t.Context(), second, config))
	require.NoError(t, os.WriteFile(filepath.Join(Path(first), "README.md"), []byte("edited"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(Path(first), "untracked"), []byte("private"), 0o644))
	runGit(t, Path(first), "add", "README.md")
	runGit(t, Path(first), "commit", "-s", "-m", "agent change")
	newCommit := runGit(t, Path(first), "rev-parse", "HEAD")
	require.NotEqual(t, commit, newCommit)
	require.NoError(t, Initialize(t.Context(), first, config))
	require.Equal(t, newCommit, runGit(t, Path(first), "rev-parse", "HEAD"))
	require.FileExists(t, filepath.Join(Path(first), "untracked"))
	content, err := os.ReadFile(filepath.Join(Path(second), "README.md"))
	require.NoError(t, err)
	require.Equal(t, "initial repository instructions\n", string(content))
	require.Equal(t, commit, runGit(t, Path(second), "rev-parse", "HEAD"))

	require.NoError(t, os.RemoveAll(filepath.Join(Path(first), ".git")))
	require.NoError(t, Initialize(t.Context(), first, config))
	require.NoDirExists(t, filepath.Join(Path(first), ".git"))
	require.NoError(t, os.RemoveAll(Path(first)))
	require.NoError(t, Initialize(t.Context(), first, config))
	require.NoDirExists(t, Path(first))
	require.Equal(t, before, requests.Load(), "restores must never access the source")
}

func TestInitializeObjectFormats(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			fixture := repository(t, true, format)
			work := filepath.Join(fixture.root, "work")
			runGit(t, work, "tag", "-a", "v1", "-m", "annotated tag")
			runGit(t, work, "push", filepath.Join(fixture.root, "repo.git"), "refs/tags/v1")
			for _, ref := range []string{"main", "refs/heads/main", "refs/tags/v1", fixture.commit} {
				t.Run(ref, func(t *testing.T) {
					fixture.config.Ref = ref
					root := t.TempDir()
					require.NoError(t, Initialize(t.Context(), root, fixture.config))
					require.Equal(t, fixture.commit, runGit(t, Path(root), "rev-parse", "HEAD"))
					require.Equal(t, format, runGit(t, Path(root), "rev-parse", "--show-object-format"))
				})
			}
		})
	}
}

func TestMovingBranchIsResolvedOnlyDuringPreparation(t *testing.T) {
	fixture := repository(t, false, "sha1")
	seed := t.TempDir()
	require.NoError(t, Initialize(t.Context(), seed, fixture.config))
	work := filepath.Join(fixture.root, "work")
	require.NoError(t, os.WriteFile(filepath.Join(work, "README.md"), []byte("next revision"), 0o644))
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-s", "-m", "advance main")
	next := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, work, "push", filepath.Join(fixture.root, "repo.git"), "HEAD:main")
	before := fixture.requests.Load()
	restored := t.TempDir()
	require.NoError(t, os.CopyFS(restored, os.DirFS(seed)))
	require.NoError(t, Initialize(t.Context(), restored, fixture.config))
	require.Equal(t, fixture.commit, runGit(t, Path(restored), "rev-parse", "HEAD"))
	require.Equal(t, before, fixture.requests.Load())
	fresh := t.TempDir()
	require.NoError(t, Initialize(t.Context(), fresh, fixture.config))
	require.Equal(t, next, runGit(t, Path(fresh), "rev-parse", "HEAD"))
	pinned := t.TempDir()
	require.NoError(t, Initialize(t.Context(), pinned, &configv1.GitWorkspaceConfig{Url: fixture.config.Url, Ref: fixture.commit}))
	require.Equal(t, fixture.commit, runGit(t, Path(pinned), "rev-parse", "HEAD"), "a full SHA remains reproducible after the branch moves")
}

func TestInitializeRecoversPublication(t *testing.T) {
	for _, phase := range []string{"staged", "published"} {
		t.Run(phase, func(t *testing.T) {
			fixture := repository(t, false, "sha1")
			config, server, commit := fixture.config, fixture.server, fixture.commit
			root := t.TempDir()
			require.NoError(t, Initialize(t.Context(), root, config))
			server.Close()
			state := filepath.Join(root, stateDirectory)
			require.NoError(t, os.Rename(filepath.Join(state, "complete.json"), filepath.Join(state, "prepared.json")))
			if phase == "staged" {
				require.NoError(t, os.Rename(Path(root), filepath.Join(state, "staging")))
			}
			require.NoError(t, Initialize(t.Context(), root, config))
			require.Equal(t, commit, runGit(t, Path(root), "rev-parse", "HEAD"))
			require.FileExists(t, filepath.Join(state, "complete.json"))
		})
	}
}

func TestInitializeFailuresPreserveFiles(t *testing.T) {
	config := repository(t, false, "sha1").config
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
		want  string
	}{
		{"nonempty", func(t *testing.T, root string) {
			require.NoError(t, os.Mkdir(Path(root), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(Path(root), "keep"), []byte("keep"), 0o644))
		}, "nonempty"},
		{"symlink", func(t *testing.T, root string) {
			require.NoError(t, os.Symlink(t.TempDir(), Path(root)))
		}, "empty directory"},
		{"receipt", func(t *testing.T, root string) {
			require.NoError(t, os.Mkdir(filepath.Join(root, stateDirectory), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(root, stateDirectory, "complete.json"), []byte("broken"), 0o600))
		}, "decode workspace receipt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.setup(t, root)
			require.ErrorContains(t, Initialize(t.Context(), root, config), test.want)
			if test.name == "nonempty" {
				require.FileExists(t, filepath.Join(Path(root), "keep"))
			}
		})
	}
	root := t.TempDir()
	config.Ref = "missing-branch"
	require.Error(t, Initialize(t.Context(), root, config))
	require.NoFileExists(t, filepath.Join(root, stateDirectory, "complete.json"))
	config.Ref = "main"
	require.NoError(t, Initialize(t.Context(), root, config), "partial fetch must be retryable")
}

func TestInitializeRejectsCorruptPreparedCheckout(t *testing.T) {
	fixture := repository(t, false, "sha1")
	root := t.TempDir()
	require.NoError(t, Initialize(t.Context(), root, fixture.config))
	state := filepath.Join(root, stateDirectory)
	require.NoError(t, os.Rename(filepath.Join(state, "complete.json"), filepath.Join(state, "prepared.json")))
	staging := filepath.Join(state, "staging")
	require.NoError(t, os.Rename(Path(root), staging))
	runGit(t, staging, "commit", "-s", "--allow-empty", "-m", "unexpected change")
	require.ErrorContains(t, Initialize(t.Context(), root, fixture.config), "does not match unfinished initialization")
	require.NoDirExists(t, Path(root))
	require.DirExists(t, staging)
}

func TestGitCancellation(t *testing.T) {
	root := t.TempDir()
	// A subprocess with a child that holds stdout open must not delay shutdown.
	require.NoError(t, os.WriteFile(filepath.Join(root, "git"), []byte("#!/bin/sh\nsleep 60 &\nwait\n"), 0o755))
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := git(ctx, root, "fetch")
	require.Error(t, err)
	require.Less(t, time.Since(started), 3*time.Second)
}

func TestValidate(t *testing.T) {
	for _, test := range []struct {
		url, ref string
		valid    bool
	}{
		{"https://github.com/org/repo.git", "main", true},
		{"https://github.com/org/repo.git", "refs/tags/v1.2", true},
		{"https://github.com/org/repo.git", strings.Repeat("a", 40), true},
		{"https://user:token@github.com/repo", "main", false},
		{"https://github.com/repo?token=secret", "main", false},
		{"https://github.com/repo#fragment", "main", false},
		{"file:///repo", "main", false},
		{"ssh://git@github.com/repo", "main", false},
		{"https://github.com/repo", "--upload-pack=evil", false},
		{"https://github.com/repo", "a/../b", false},
		{"https://github.com/repo", "a.lock", false},
		{"https://github.com/repo", "main:other", false},
	} {
		t.Run(test.url+"/"+test.ref, func(t *testing.T) {
			err := Validate(&configv1.GitWorkspaceConfig{Url: test.url, Ref: test.ref})
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
