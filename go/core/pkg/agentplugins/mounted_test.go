package agentplugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
)

func TestMaterializeMountedSelections(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"standalone/SKILL.md":             "# Standalone",
		"standalone/run.sh":               "#!/bin/sh\necho mounted-skill\n",
		"plugin/plugin.json":              `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"mounted"}`,
		"plugin/skills/selected/SKILL.md": "# Selected",
		"plugin/skills/disabled/SKILL.md": "# Disabled",
	}
	for path, content := range files {
		path = filepath.Join(root, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	require.NoError(t, os.Chmod(filepath.Join(root, "standalone/run.sh"), 0o755))
	require.NoError(t, os.Symlink("run.sh", filepath.Join(root, "standalone/alias.sh")))
	// Unselected image contents do not belong to either package.
	require.NoError(t, os.Symlink("missing", filepath.Join(root, "unrelated-link")))
	oversized, err := os.Create(filepath.Join(root, "unrelated-large-file"))
	require.NoError(t, err)
	require.NoError(t, oversized.Truncate(maxPackageBytes+1))
	require.NoError(t, oversized.Close())
	paths := Paths{Packages: filepath.Join(t.TempDir(), "packages"), Skills: filepath.Join(t.TempDir(), "skills")}
	resources := agentplugin.Resources{
		Skills:  []agentplugin.Skill{{Name: "renamed", Source: agentplugin.Source{Directory: root, Path: "standalone"}}},
		Plugins: []agentplugin.Bundle{{Source: agentplugin.Source{Directory: root, Path: "plugin"}, Skills: []string{"selected"}}},
	}
	for range 2 {
		_, err := Materialize(t.Context(), resources, paths)
		require.NoError(t, err)
	}
	for path, want := range map[string]string{"renamed/SKILL.md": "# Standalone", "selected/SKILL.md": "# Selected", "renamed/alias.sh": files["standalone/run.sh"]} {
		got, err := os.ReadFile(filepath.Join(paths.Skills, path))
		require.NoError(t, err)
		require.Equal(t, want, string(got))
	}
	info, err := os.Stat(filepath.Join(paths.Skills, "renamed/run.sh"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	require.NoDirExists(t, filepath.Join(paths.Skills, "disabled"))
	packages, err := os.ReadDir(paths.Packages)
	require.NoError(t, err)
	require.Empty(t, packages, "mounted packages must not be extracted or cached")
	for path, want := range files {
		got, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		require.Equal(t, want, string(got), "mounted source changed: %s", path)
	}
}

func TestMaterializeMountedSourceFailures(t *testing.T) {
	for _, tt := range []struct {
		name      string
		prepare   func(*testing.T, string) agentplugin.Source
		wantError string
	}{
		{name: "missing mount", prepare: func(t *testing.T, root string) agentplugin.Source {
			return agentplugin.Source{Directory: filepath.Join(root, "missing")}
		}, wantError: "validate mounted artifact"},
		{name: "relative mount", prepare: func(t *testing.T, root string) agentplugin.Source {
			return agentplugin.Source{Directory: "relative"}
		}, wantError: "must be absolute"},
		{name: "missing path", prepare: func(t *testing.T, root string) agentplugin.Source {
			return agentplugin.Source{Directory: root, Path: "missing"}
		}, wantError: "no such file"},
		{name: "missing skill", prepare: func(t *testing.T, root string) agentplugin.Source {
			return agentplugin.Source{Directory: root}
		}, wantError: "SKILL.md is required"},
		{name: "traversal", prepare: func(t *testing.T, root string) agentplugin.Source {
			return agentplugin.Source{Directory: root, Path: "../outside"}
		}, wantError: "is not relative"},
		{name: "escaping symlink", prepare: func(t *testing.T, root string) agentplugin.Source {
			require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "outside")))
			return agentplugin.Source{Directory: root, Path: "outside"}
		}, wantError: "escapes its root"},
		{name: "symlink outside selected package", prepare: func(t *testing.T, root string) agentplugin.Source {
			require.NoError(t, os.Mkdir(filepath.Join(root, "selected"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "outside"), []byte("outside"), 0o644))
			require.NoError(t, os.Symlink("../outside", filepath.Join(root, "selected/link")))
			return agentplugin.Source{Directory: root, Path: "selected"}
		}, wantError: "escapes artifact root"},
		{name: "multiple sources", prepare: func(t *testing.T, root string) agentplugin.Source {
			return agentplugin.Source{Directory: root, Git: &agentplugin.GitSource{URL: "unused"}}
		}, wantError: "exactly one artifact source"},
		{name: "oversized skill", prepare: func(t *testing.T, root string) agentplugin.Source {
			require.NoError(t, os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# Skill"), 0o644))
			f, err := os.Create(filepath.Join(root, "oversized"))
			require.NoError(t, err)
			require.NoError(t, f.Truncate(maxPackageBytes+1))
			require.NoError(t, f.Close())
			return agentplugin.Source{Directory: root}
		}, wantError: "artifact exceeds"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := tt.prepare(t, t.TempDir())
			paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
			// A previous extraction must not hide a missing or invalid mount.
			cached := filepath.Join(paths.Packages, "standalone-0")
			require.NoError(t, os.Mkdir(cached, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(cached, "SKILL.md"), []byte("# Stale"), 0o644))
			_, err := Materialize(t.Context(), agentplugin.Resources{Skills: []agentplugin.Skill{{Name: "skill", Source: source}}}, paths)
			require.ErrorContains(t, err, tt.wantError)
			require.NoDirExists(t, filepath.Join(paths.Skills, "skill"))
		})
	}
}

func TestMaterializeMountedClaudePlugin(t *testing.T) {
	// Exercise an aliased parent on every platform, as with /var on macOS.
	parent := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(parent, alias))
	root := filepath.Join(alias, "plugin")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".claude-plugin/plugin.json"), []byte(`{"name":"mounted-native"}`), 0o644))
	materialized, err := Materialize(t.Context(), agentplugin.Resources{Plugins: []agentplugin.Bundle{{Source: agentplugin.Source{Directory: root}}}}, Paths{Packages: t.TempDir(), Skills: t.TempDir()})
	require.NoError(t, err)
	root, err = filepath.EvalSymlinks(root)
	require.NoError(t, err)
	require.Equal(t, []string{root}, materialized.ClaudeFormatPluginRoots())
}

func TestMaterializationBinDirs(t *testing.T) {
	t.Run("Given mounted plugins with and without a bin directory, when they are materialized, assert that BinDirs lists each bin directory in plugin order", func(t *testing.T) {
		root := t.TempDir()
		writeMountedPlugin(t, root, "first", map[string]string{"bin/first-tool": "#!/bin/sh\n"})
		writeMountedPlugin(t, root, "no-bin", nil)
		writeMountedPlugin(t, root, "bin-file", map[string]string{"bin": "not a directory"})
		writeMountedPlugin(t, root, "second", map[string]string{"bin/second-tool": "#!/bin/sh\n"})
		var bundles []agentplugin.Bundle
		for _, name := range []string{"first", "no-bin", "bin-file", "second"} {
			bundles = append(bundles, agentplugin.Bundle{Source: agentplugin.Source{Directory: root, Path: name}})
		}

		materialized, err := Materialize(t.Context(), agentplugin.Resources{Plugins: bundles}, Paths{Packages: t.TempDir(), Skills: t.TempDir()})

		require.NoError(t, err)
		resolved, err := filepath.EvalSymlinks(root)
		require.NoError(t, err)
		require.Equal(t, []string{filepath.Join(resolved, "first", "bin"), filepath.Join(resolved, "second", "bin")}, materialized.BinDirs())
	})
}

func TestMaterializeMountedPluginIgnoresPackageLimits(t *testing.T) {
	for _, tt := range []struct {
		name    string
		prepare func(*testing.T, string)
	}{
		{name: "Given a mounted plugin over the byte limit, when it is materialized, assert that it and its selected skill load", prepare: func(t *testing.T, plugin string) {
			createSparseFile(t, filepath.Join(plugin, "bin", "toolchain"), maxPackageBytes+1)
		}},
		{name: "Given a mounted plugin over the entry limit, when it is materialized, assert that it and its selected skill load", prepare: func(t *testing.T, plugin string) {
			createEmptyFiles(t, filepath.Join(plugin, "share"), maxPackageEntries+1)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			plugin := writeMountedPlugin(t, root, "toolchain", map[string]string{"bin/tool": "#!/bin/sh\n", "skills/review/SKILL.md": "# Review"})
			tt.prepare(t, plugin)
			paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}

			materialized, err := Materialize(t.Context(), agentplugin.Resources{Plugins: []agentplugin.Bundle{{
				Source: agentplugin.Source{Directory: root, Path: "toolchain"}, Skills: []string{"review"},
			}}}, paths)

			require.NoError(t, err)
			require.Len(t, materialized.BinDirs(), 1)
			require.FileExists(t, filepath.Join(paths.Skills, "review", "SKILL.md"))
		})
	}
}

func TestMaterializeKeepsPackageLimitsOutsideMountedPlugins(t *testing.T) {
	for _, tt := range []struct {
		name      string
		prepare   func(*testing.T, string, Paths) agentplugin.Bundle
		wantError string
	}{
		{name: "Given a fetched plugin over the byte limit, when it is materialized, assert that it is rejected", prepare: func(t *testing.T, _ string, paths Paths) agentplugin.Bundle {
			cached := writeMountedPlugin(t, paths.Packages, "plugin-0", nil)
			createSparseFile(t, filepath.Join(cached, "bin", "toolchain"), maxPackageBytes+1)
			return agentplugin.Bundle{Source: agentplugin.Source{Git: &agentplugin.GitSource{URL: "unused", Commit: strings.Repeat("a", 40)}}}
		}, wantError: "artifact exceeds"},
		{name: "Given a fetched plugin over the entry limit, when it is materialized, assert that it is rejected", prepare: func(t *testing.T, _ string, paths Paths) agentplugin.Bundle {
			cached := writeMountedPlugin(t, paths.Packages, "plugin-0", nil)
			createEmptyFiles(t, filepath.Join(cached, "share"), maxPackageEntries+1)
			return agentplugin.Bundle{Source: agentplugin.Source{S3: &agentplugin.S3Source{Bucket: "unused", Key: "unused"}}}
		}, wantError: "filesystem entries"},
		{name: "Given a mounted plugin whose selected skill is over the byte limit, when it is materialized, assert that the skill is rejected", prepare: func(t *testing.T, root string, _ Paths) agentplugin.Bundle {
			plugin := writeMountedPlugin(t, root, "plugin", map[string]string{"skills/review/SKILL.md": "# Review"})
			createSparseFile(t, filepath.Join(plugin, "skills", "review", "large"), maxPackageBytes+1)
			return agentplugin.Bundle{Source: agentplugin.Source{Directory: root, Path: "plugin"}, Skills: []string{"review"}}
		}, wantError: "artifact exceeds"},
		{name: "Given a mounted plugin with a symlink outside the plugin, when it is materialized, assert that it is rejected", prepare: func(t *testing.T, root string, _ Paths) agentplugin.Bundle {
			plugin := writeMountedPlugin(t, root, "plugin", nil)
			require.NoError(t, os.WriteFile(filepath.Join(root, "outside"), []byte("outside"), 0o644))
			require.NoError(t, os.Symlink("../outside", filepath.Join(plugin, "link")))
			return agentplugin.Bundle{Source: agentplugin.Source{Directory: root, Path: "plugin"}}
		}, wantError: "escapes artifact root"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
			bundle := tt.prepare(t, t.TempDir(), paths)

			_, err := Materialize(t.Context(), agentplugin.Resources{Plugins: []agentplugin.Bundle{bundle}}, paths)

			require.ErrorContains(t, err, tt.wantError)
			require.NoDirExists(t, filepath.Join(paths.Skills, "review"))
		})
	}
}

func writeMountedPlugin(t *testing.T, root, directory string, files map[string]string) string {
	t.Helper()
	plugin := filepath.Join(root, directory)
	require.NoError(t, os.MkdirAll(plugin, 0o755))
	manifest := `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"` + directory + `"}`
	require.NoError(t, os.WriteFile(filepath.Join(plugin, "plugin.json"), []byte(manifest), 0o644))
	for path, content := range files {
		path = filepath.Join(plugin, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	}
	return plugin
}

func createSparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	file, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, file.Truncate(size))
	require.NoError(t, file.Close())
}

func createEmptyFiles(t *testing.T, directory string, count int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(directory, 0o755))
	for i := range count {
		require.NoError(t, os.WriteFile(filepath.Join(directory, fmt.Sprintf("file-%d", i)), nil, 0o644))
	}
}
