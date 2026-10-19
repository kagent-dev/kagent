package agentplugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
)

func TestMaterializeMountedPackageRejectsNamedPipe(t *testing.T) {
	for _, tt := range []struct {
		name      string
		skillPath string
		pipePath  string
	}{
		{name: "root", pipePath: "pipe"},
		{name: "git metadata", pipePath: ".git/pipe"},
		{name: "nested git metadata", skillPath: "skills/review", pipePath: "skills/review/.git/pipe"},
		{name: "selection inside git", skillPath: ".git/review", pipePath: ".git/review/pipe"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			skill := filepath.Join(root, tt.skillPath)
			require.NoError(t, os.MkdirAll(skill, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# Review"), 0o644))
			pipe := filepath.Join(root, tt.pipePath)
			require.NoError(t, os.MkdirAll(filepath.Dir(pipe), 0o755))
			require.NoError(t, unix.Mkfifo(pipe, 0o600))
			// Check rejection before materializing so a regression fails without
			// blocking the test on the FIFO in copySkill.
			require.ErrorContains(t, validatePackage(root), "unsupported file")
			paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
			_, err := Materialize(t.Context(), agentplugin.Resources{Skills: []agentplugin.Skill{{
				Name: "review", Source: agentplugin.Source{Directory: root, Path: tt.skillPath},
			}}}, paths)
			require.ErrorContains(t, err, "unsupported file")
			require.NoDirExists(t, filepath.Join(paths.Skills, "review"))
		})
	}
}

func TestMaterializeMountedPackageIgnoresUnselectedNamedPipe(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "selected"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "selected/SKILL.md"), []byte("# Selected"), 0o644))
	require.NoError(t, unix.Mkfifo(filepath.Join(root, "unrelated-pipe"), 0o600))
	paths := Paths{Packages: t.TempDir(), Skills: t.TempDir()}
	_, err := Materialize(t.Context(), agentplugin.Resources{Skills: []agentplugin.Skill{{
		Name: "selected", Source: agentplugin.Source{Directory: root, Path: "selected"},
	}}}, paths)
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(paths.Skills, "selected/SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "# Selected", string(content))
}
