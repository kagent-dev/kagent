package skillsinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_applySubPath_rejectsTraversal exercises the validation gate without
// invoking `cp`. We give it a clean dest tree with a real subdir then ask
// for traversal — the function must error before touching the filesystem.
func Test_applySubPath_rejectsTraversal(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dest, "real"), 0o755))

	cases := []string{
		"../escape",
		"/etc",
		"a/../../escape",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			err := applySubPath(dest, p)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid subPath")
		})
	}
}

// Test_applySubPath_rejectsNonDir guards against a benign-looking subPath
// that points at a file rather than a directory. Without this check the
// subsequent `cp -rL` would do something silly; the explicit error is
// clearer and matches the documented contract.
func Test_applySubPath_rejectsNonDir(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dest, "file"), []byte("x"), 0o644))

	err := applySubPath(dest, "file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestCloneGitCommitRejectsMutableRef(t *testing.T) {
	err := CloneGitCommit("https://example.com/repository.git", "main", t.TempDir())
	require.ErrorContains(t, err, "full SHA")
}

func TestCloneGitSkipsExistingDestination(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "existing-skill")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	existingSkill := filepath.Join(dest, "SKILL.md")
	require.NoError(t, os.WriteFile(existingSkill, []byte("existing skill\n"), 0o644))

	err := CloneGit(GitRef{
		URL:  filepath.Join(t.TempDir(), "unreachable-source"),
		Ref:  "main",
		Dest: dest,
	})

	require.NoError(t, err)
	content, err := os.ReadFile(existingSkill)
	require.NoError(t, err)
	assert.Equal(t, "existing skill\n", string(content))
}

func TestCloneGitRejectsExistingDestinationWithoutSkill(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "incomplete-skill")
	require.NoError(t, os.MkdirAll(dest, 0o755))

	err := CloneGit(GitRef{
		URL:  filepath.Join(t.TempDir(), "unreachable-source"),
		Ref:  "main",
		Dest: dest,
	})

	require.ErrorContains(t, err, "existing git destination is not a skill")
}

func TestExistingGitSkillRejectsSymlinks(t *testing.T) {
	t.Run("destination", func(t *testing.T) {
		target := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("skill\n"), 0o644))
		destination := filepath.Join(t.TempDir(), "skill")
		require.NoError(t, os.Symlink(target, destination))

		_, err := existingGitSkill(destination)
		require.ErrorContains(t, err, "symbolic link")
	})

	t.Run("skill file", func(t *testing.T) {
		destination := t.TempDir()
		target := filepath.Join(t.TempDir(), "SKILL.md")
		require.NoError(t, os.WriteFile(target, []byte("skill\n"), 0o644))
		require.NoError(t, os.Symlink(target, filepath.Join(destination, "SKILL.md")))

		_, err := existingGitSkill(destination)
		require.ErrorContains(t, err, "symbolic link")
	})
}

func TestCloneGitFailedSubPathRetryDoesNotAcceptRootSkill(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, output)
	}
	runGit("", "init", "--initial-branch", "main", source)
	runGit(source, "config", "user.email", "test@example.com")
	runGit(source, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("root skill\n"), 0o644))
	runGit(source, "add", "SKILL.md")
	runGit(source, "commit", "-m", "root skill")

	ref := GitRef{
		URL:     source,
		Ref:     "main",
		Dest:    filepath.Join(root, "destination"),
		SubPath: "missing-skill",
	}
	require.Error(t, CloneGit(ref), "first attempt unexpectedly succeeded")
	assert.NoDirExists(t, ref.Dest, "failed attempt left a completed-looking destination")

	require.NoError(t, os.MkdirAll(filepath.Join(source, "missing-skill"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "missing-skill", "SKILL.md"), []byte("requested skill\n"), 0o644))
	runGit(source, "add", "missing-skill/SKILL.md")
	runGit(source, "commit", "-m", "requested skill")

	require.NoError(t, CloneGit(ref), "retry did not recover after the requested subpath became available")
	content, err := os.ReadFile(filepath.Join(ref.Dest, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, "requested skill\n", string(content))
}

func TestCloneGitReplacesLegacyFailedSubPathCheckout(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, output)
	}
	runGit("", "init", "--initial-branch", "main", source)
	runGit(source, "config", "user.email", "test@example.com")
	runGit(source, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("legacy root skill\n"), 0o644))
	runGit(source, "add", "SKILL.md")
	runGit(source, "commit", "-m", "root skill")

	destination := filepath.Join(root, "destination")
	runGit("", "clone", "--", source, destination)

	require.NoError(t, os.MkdirAll(filepath.Join(source, "requested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "requested", "SKILL.md"), []byte("requested skill\n"), 0o644))
	runGit(source, "add", "requested/SKILL.md")
	runGit(source, "commit", "-m", "requested skill")

	err := CloneGit(GitRef{URL: source, Ref: "main", Dest: destination, SubPath: "requested"})
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(destination, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, "requested skill\n", string(content))
	assert.NoDirExists(t, filepath.Join(destination, ".git"))
}

func TestCloneGitRejectsStagedSubPathWithoutSkill(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, output)
	}
	runGit("", "init", "--initial-branch", "main", source)
	runGit(source, "config", "user.email", "test@example.com")
	runGit(source, "config", "user.name", "Test User")
	require.NoError(t, os.MkdirAll(filepath.Join(source, "requested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "requested", "README.md"), []byte("not a skill\n"), 0o644))
	runGit(source, "add", "requested/README.md")
	runGit(source, "commit", "-m", "directory without skill metadata")

	destination := filepath.Join(root, "destination")
	err := CloneGit(GitRef{URL: source, Ref: "main", Dest: destination, SubPath: "requested"})
	require.ErrorContains(t, err, "SKILL.md")
	assert.NoDirExists(t, destination)
}

func TestCloneGitFailedSubPathPreservesValidExistingSkill(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, output)
	}
	runGit("", "init", "--initial-branch", "main", source)
	runGit(source, "config", "user.email", "test@example.com")
	runGit(source, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("source skill\n"), 0o644))
	runGit(source, "add", "SKILL.md")
	runGit(source, "commit", "-m", "root skill")

	destination := filepath.Join(root, "destination")
	require.NoError(t, os.MkdirAll(destination, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(destination, "SKILL.md"), []byte("existing completion\n"), 0o644))

	err := CloneGit(GitRef{URL: source, Ref: "main", Dest: destination, SubPath: "missing"})
	require.Error(t, err)
	content, readErr := os.ReadFile(filepath.Join(destination, "SKILL.md"))
	require.NoError(t, readErr)
	assert.Equal(t, "existing completion\n", string(content))
}
