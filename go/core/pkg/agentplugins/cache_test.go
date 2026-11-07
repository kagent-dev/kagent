package agentplugins

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/internal/testutil"
)

func TestMaterializeReusesReorderedSourcesOffline(t *testing.T) {
	repository, commit := testutil.CommitFiles(t, map[string]string{
		"first/SKILL.md":                      "# First",
		"second/SKILL.md":                     "# Second",
		"plugin-a/.claude-plugin/plugin.json": `{"name":"plugin-a"}`,
		"plugin-b/.claude-plugin/plugin.json": `{"name":"plugin-b"}`,
	})
	source := func(path string) agentplugin.Source {
		return agentplugin.Source{Git: &agentplugin.GitSource{URL: repository, Commit: commit}, Path: path}
	}
	resources := agentplugin.Resources{
		Skills: []agentplugin.Skill{
			{Name: "first", Source: source("first")},
			{Name: "second", Source: source("second")},
			{Name: "alias", Source: source("first")},
		},
		Plugins: []agentplugin.Bundle{{Source: source("plugin-a")}, {Source: source("plugin-b")}},
	}
	paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
	initial, err := Materialize(t.Context(), resources, paths)
	require.NoError(t, err)
	require.NoError(t, os.Rename(repository, filepath.Join(t.TempDir(), "offline")))
	slices.Reverse(resources.Skills)
	slices.Reverse(resources.Plugins)

	reordered, err := Materialize(t.Context(), resources, paths)

	require.NoError(t, err)
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
		name string
		path string
	}{
		{name: "Given an unavailable commit, when fetching a new pin, assert that the previous cache survives", path: "missing-commit"},
		{name: "Given a package without a skill, when fetching it, assert that it is not published", path: "incomplete"},
		{name: "Given a missing selected directory, when fetching it, assert that it is not published", path: "missing-path"},
		{name: "Given a traversal path, when fetching it, assert that it is not published", path: "../outside"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repository, commit := testutil.CommitFiles(t, map[string]string{"SKILL.md": "# Current", "incomplete/README.md": "No skill"})
			source := agentplugin.Source{Git: &agentplugin.GitSource{URL: repository, Commit: commit}}
			resources := agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "review", Source: source}}}
			paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
			_, err := Materialize(t.Context(), resources, paths)
			require.NoError(t, err)
			initial, err := os.ReadDir(paths.Packages)
			require.NoError(t, err)
			invalid := source
			invalid.Path = scenario.path
			if scenario.path == "missing-commit" {
				invalid.Git = &agentplugin.GitSource{URL: repository, Commit: strings.Repeat("f", 40)}
			}

			_, err = Materialize(t.Context(), agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "review", Source: invalid}}}, paths)

			require.Error(t, err)
			remaining, err := os.ReadDir(paths.Packages)
			require.NoError(t, err)
			require.Len(t, remaining, 1, "failed downloads must not leave published or staged entries")
			require.Equal(t, initial[0].Name(), remaining[0].Name())
			require.NoError(t, os.Rename(repository, filepath.Join(t.TempDir(), "offline")))
			_, err = Materialize(t.Context(), resources, paths)
			require.NoError(t, err)
			content, err := os.ReadFile(filepath.Join(paths.Skills, "review", "SKILL.md"))
			require.NoError(t, err)
			require.Equal(t, "# Current", string(content))
		})
	}
}

func TestMaterializeRepairsIncompleteCache(t *testing.T) {
	repository, commit := testutil.CommitFiles(t, map[string]string{"selected/SKILL.md": "# Current"})
	resources := agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "review", Source: agentplugin.Source{
		Git: &agentplugin.GitSource{URL: repository, Commit: commit}, Path: "selected",
	}}}}
	paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
	_, err := Materialize(t.Context(), resources, paths)
	require.NoError(t, err)
	entries, err := os.ReadDir(paths.Packages)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NoError(t, os.Remove(filepath.Join(paths.Packages, entries[0].Name(), "selected", "SKILL.md")))

	_, err = Materialize(t.Context(), resources, paths)

	require.NoError(t, err)
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

func TestMaterializeS3Versions(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	archives := make(map[string][]byte)
	for version, content := range map[string]string{"first": "# First", "second": "# Second"} {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		file, err := writer.Create("SKILL.md")
		require.NoError(t, err)
		_, err = file.Write([]byte(content))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		archives[version] = buffer.Bytes()
	}
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		version := request.URL.Query().Get("versionId")
		archive, ok := archives[version]
		if request.URL.Path != "/skills/review.zip" || !ok {
			http.NotFound(response, request)
			return
		}
		requests <- version
		_, _ = response.Write(archive)
	}))
	t.Cleanup(server.Close)
	resources := agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "review", Source: agentplugin.Source{S3: &agentplugin.S3Source{
		Endpoint: server.URL, Bucket: "skills", Key: "review.zip", VersionID: "first", Region: "us-east-1",
	}}}}}
	paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
	for range 2 {
		_, err := Materialize(t.Context(), resources, paths)
		require.NoError(t, err)
	}
	require.Len(t, requests, 1, "the same S3 version must reuse its cached archive")
	require.Equal(t, "first", <-requests)
	resources.Skills[0].Source.S3.VersionID = "second"

	_, err := Materialize(t.Context(), resources, paths)

	require.NoError(t, err)
	require.Len(t, requests, 1)
	require.Equal(t, "second", <-requests)
	server.Close()
	_, err = Materialize(t.Context(), resources, paths)
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(paths.Skills, "review", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "# Second", string(content))
	entries, err := os.ReadDir(paths.Packages)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}
