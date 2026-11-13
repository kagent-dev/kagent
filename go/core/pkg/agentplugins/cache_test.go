package agentplugins

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
)

func TestMaterializeReusesReorderedSources(t *testing.T) {
	store := newPackageStore(map[string]revision{"pinned": {files: map[string]string{
		"first/SKILL.md":                      "# First",
		"second/SKILL.md":                     "# Second",
		"plugin-a/.claude-plugin/plugin.json": `{"name":"plugin-a"}`,
		"plugin-b/.claude-plugin/plugin.json": `{"name":"plugin-b"}`,
	}}})
	resources := agentplugin.Resources{
		Skills: []agentplugin.Skill{
			{Name: "first", Source: gitSource("pinned", "first")},
			{Name: "second", Source: gitSource("pinned", "second")},
			{Name: "alias", Source: gitSource("pinned", "first")},
		},
		Plugins: []agentplugin.Bundle{{Source: gitSource("pinned", "plugin-a")}, {Source: gitSource("pinned", "plugin-b")}},
	}
	paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
	initial, err := store.materialize(t, resources, paths)
	require.NoError(t, err)
	require.Len(t, store.fetched, 4, "identical sources share a fetch")
	slices.Reverse(resources.Skills)
	slices.Reverse(resources.Plugins)

	reordered, err := store.materialize(t, resources, paths)

	require.NoError(t, err)
	require.Len(t, store.fetched, 4, "cached sources must not be fetched again")
	require.ElementsMatch(t, initial.ClaudeFormatPluginRoots(), reordered.ClaudeFormatPluginRoots())
	for name, want := range map[string]string{"first": "# First", "second": "# Second", "alias": "# First"} {
		content, err := os.ReadFile(filepath.Join(paths.Skills, name, "SKILL.md"))
		require.NoError(t, err)
		require.Equal(t, want, string(content))
	}
	entries, err := os.ReadDir(paths.Packages)
	require.NoError(t, err)
	require.Len(t, entries, 4, "identical sources share a cache entry")
}

func TestMaterializeDoesNotPublishFailedFetch(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		revision string
		path     string
	}{
		{name: "Given an unavailable revision, when fetching a new pin, assert that the previous cache survives", revision: "missing"},
		{name: "Given a fetch that fails after writing files, when fetching it, assert that it is not published", revision: "partial"},
		{name: "Given a package without a skill, when fetching it, assert that it is not published", revision: "current", path: "incomplete"},
		{name: "Given a missing selected directory, when fetching it, assert that it is not published", revision: "current", path: "missing-path"},
		{name: "Given a traversal path, when fetching it, assert that it is not published", revision: "current", path: "../outside"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store := newPackageStore(map[string]revision{
				"current": {files: map[string]string{"SKILL.md": "# Current", "incomplete/README.md": "No skill"}},
				"partial": {files: map[string]string{"SKILL.md": "# Partial"}, err: errors.New("connection reset")},
			})
			resources := agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "review", Source: gitSource("current", "")}}}
			paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
			_, err := store.materialize(t, resources, paths)
			require.NoError(t, err)
			initial, err := os.ReadDir(paths.Packages)
			require.NoError(t, err)

			_, err = store.materialize(t, agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "review", Source: gitSource(scenario.revision, scenario.path)}}}, paths)

			require.Error(t, err)
			remaining, err := os.ReadDir(paths.Packages)
			require.NoError(t, err)
			require.Len(t, remaining, 1, "failed downloads must not leave published or staged entries")
			require.Equal(t, initial[0].Name(), remaining[0].Name())
			fetched := len(store.fetched)
			_, err = store.materialize(t, resources, paths)
			require.NoError(t, err)
			require.Len(t, store.fetched, fetched, "the previous cache entry must be reused")
			content, err := os.ReadFile(filepath.Join(paths.Skills, "review", "SKILL.md"))
			require.NoError(t, err)
			require.Equal(t, "# Current", string(content))
		})
	}
}

func TestMaterializeRepairsIncompleteCache(t *testing.T) {
	store := newPackageStore(map[string]revision{"pinned": {files: map[string]string{"selected/SKILL.md": "# Current"}}})
	resources := agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "review", Source: gitSource("pinned", "selected")}}}
	paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
	_, err := store.materialize(t, resources, paths)
	require.NoError(t, err)
	entries, err := os.ReadDir(paths.Packages)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NoError(t, os.Remove(filepath.Join(paths.Packages, entries[0].Name(), "selected", "SKILL.md")))

	_, err = store.materialize(t, resources, paths)

	require.NoError(t, err)
	require.Equal(t, []string{"pinned", "pinned"}, store.fetched)
	content, err := os.ReadFile(filepath.Join(paths.Skills, "review", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "# Current", string(content))
}

func TestMaterializePrunesOnlyOwnedUnusedCacheEntries(t *testing.T) {
	paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
	for _, name := range []string{"source-" + strings.Repeat("a", 64), ".fetch-interrupted", "standalone-0", "plugin-1", "subagents", "other"} {
		root := filepath.Join(paths.Packages, name)
		require.NoError(t, os.Mkdir(root, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# Previous"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(paths.Packages, "plugin-1.source"), []byte("previous"), 0o644))

	_, err := Materialize(t.Context(), agentplugin.Resources{}, paths)

	require.NoError(t, err)
	entries, err := os.ReadDir(paths.Packages)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	for _, name := range []string{"subagents", "other"} {
		content, err := os.ReadFile(filepath.Join(paths.Packages, name, "SKILL.md"))
		require.NoError(t, err)
		require.Equal(t, "# Previous", string(content))
	}
}

func TestMaterializePreservesMountedSourcesWithinCache(t *testing.T) {
	paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
	mount := filepath.Join(paths.Packages, "standalone-0", "mounted")
	require.NoError(t, os.MkdirAll(mount, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mount, "SKILL.md"), []byte("# Mounted"), 0o644))
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(mount, alias))

	_, err := Materialize(t.Context(), agentplugin.Resources{Skills: []agentplugin.Skill{{
		Name: "review", Source: agentplugin.Source{Directory: alias},
	}}}, paths)

	require.NoError(t, err)
	for _, root := range []string{mount, filepath.Join(paths.Skills, "review")} {
		content, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
		require.NoError(t, err)
		require.Equal(t, "# Mounted", string(content))
	}
}

func TestMaterializeKeysS3CacheByVersion(t *testing.T) {
	store := newPackageStore(map[string]revision{
		"first":  {files: map[string]string{"SKILL.md": "# First"}},
		"second": {files: map[string]string{"SKILL.md": "# Second"}},
	})
	source := agentplugin.S3Source{Endpoint: "https://s3.example.com", Bucket: "skills", Key: "review.zip", VersionID: "first", Region: "us-east-1"}
	resources := agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "review", Source: agentplugin.Source{S3: &source}}}}
	paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
	for range 2 {
		_, err := store.materialize(t, resources, paths)
		require.NoError(t, err)
	}
	require.Equal(t, []string{"first"}, store.fetched, "the same S3 version must reuse its cached archive")
	source.VersionID = "second"

	_, err := store.materialize(t, resources, paths)

	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, store.fetched)
	content, err := os.ReadFile(filepath.Join(paths.Skills, "review", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "# Second", string(content))
	entries, err := os.ReadDir(paths.Packages)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

// revision is what a fake fetch of one Git commit or S3 version writes before
// returning err.
type revision struct {
	files map[string]string
	err   error
}

// packageStore stands in for Git and S3 and records each fetched revision.
type packageStore struct {
	revisions map[string]revision
	fetched   []string
}

func newPackageStore(revisions map[string]revision) *packageStore {
	return &packageStore{revisions: revisions}
}

func gitSource(commit, path string) agentplugin.Source {
	return agentplugin.Source{Git: &agentplugin.GitSource{URL: "https://git.example.com/plugins.git", Commit: commit}, Path: path}
}

func (s *packageStore) materialize(t *testing.T, resources agentplugin.Resources, paths Paths) (Materialization, error) {
	t.Helper()
	return materialize(t.Context(), resources, paths, s.fetch)
}

func (s *packageStore) fetch(_ context.Context, source agentplugin.Source, staging string) error {
	var name string
	switch {
	case source.Git != nil:
		name = source.Git.Commit
	case source.S3 != nil:
		name = source.S3.VersionID
	}
	s.fetched = append(s.fetched, name)
	fetched, ok := s.revisions[name]
	if !ok {
		return fmt.Errorf("revision %q not found", name)
	}
	for path, content := range fetched.files {
		path = filepath.Join(staging, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return fetched.err
}
