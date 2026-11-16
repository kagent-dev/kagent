package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	apiv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
)

func TestSkillInstall(t *testing.T) {
	t.Run("Given a home with Claude Code, when the skill is installed, assert that the files are written and Claude Code links to them", func(t *testing.T) {
		f := newSkillFixture(t).withClaude()
		results, err := f.results("skill", "install")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: f.dir(), Status: "added"}, {Path: f.link(), Status: "linked"}}, results)
		target, err := os.Readlink(f.link())
		require.NoError(t, err)
		require.Equal(t, filepath.Join("..", "..", ".agents", "skills", "kagent"), target)
		require.Equal(t, f.skill, f.read(f.link()))
	})

	t.Run("Given an installed skill, when it is installed again, assert that both paths are unchanged", func(t *testing.T) {
		f := newSkillFixture(t).withClaude()
		_, err := f.results("skill", "install")
		require.NoError(t, err)
		results, err := f.results("skill", "install")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: f.dir(), Status: "unchanged"}, {Path: f.link(), Status: "unchanged"}}, results)
	})

	t.Run("Given an installed skill with an edited and an extra file, when it is installed again, assert that it holds exactly the embedded files", func(t *testing.T) {
		f := newSkillFixture(t).withClaude()
		_, err := f.results("skill", "install")
		require.NoError(t, err)
		f.write(f.dir(), "references/sandboxes.md", "edited\n")
		f.write(f.dir(), "notes.md", "extra\n")
		results, err := f.results("skill", "install")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: f.dir(), Status: "updated"}, {Path: f.link(), Status: "unchanged"}}, results)
		require.Equal(t, f.skill, f.read(f.dir()))
	})

	t.Run("Given a directory holding a different skill, when install runs without --force, assert that it refuses and keeps the directory", func(t *testing.T) {
		f := newSkillFixture(t)
		f.write(f.dir(), "SKILL.md", "---\nname: other\n---\n")
		_, err := f.results("skill", "install")
		require.ErrorContains(t, err, "pass --force")
		require.Equal(t, map[string]string{"SKILL.md": "---\nname: other\n---\n"}, f.read(f.dir()))
	})

	t.Run("Given a directory holding a different skill, when install runs with --force, assert that the kagent skill replaces it", func(t *testing.T) {
		f := newSkillFixture(t)
		f.write(f.dir(), "SKILL.md", "---\nname: other\n---\n")
		results, err := f.results("skill", "install", "--force")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: f.dir(), Status: "updated"}}, results)
		require.Equal(t, f.skill, f.read(f.dir()))
	})

	t.Run("Given a home without Claude Code, when the skill is installed, assert that no Claude Code path is created", func(t *testing.T) {
		f := newSkillFixture(t)
		results, err := f.results("skill", "install")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: f.dir(), Status: "added"}}, results)
		require.NoDirExists(t, filepath.Join(f.home, ".claude"))
	})

	t.Run("Given CLAUDE_CONFIG_DIR outside the home, when the skill is installed, assert that a relative link in its skills directory reaches the skill", func(t *testing.T) {
		f := newSkillFixture(t)
		claudeDir := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
		link := filepath.Join(claudeDir, "skills", "kagent")
		results, err := f.results("skill", "install")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: f.dir(), Status: "added"}, {Path: link, Status: "linked"}}, results)
		target, err := os.Readlink(link)
		require.NoError(t, err)
		require.False(t, filepath.IsAbs(target), "link target %s must be relative", target)
		require.Equal(t, f.skill, f.read(link))
	})

	t.Run("Given a git repository, when install --project runs in a subdirectory, assert that the skill and link go under the top level", func(t *testing.T) {
		f := newSkillFixture(t).withClaude()
		repo, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, exec.Command("git", "init", "--quiet", repo).Run())
		require.NoError(t, os.Mkdir(filepath.Join(repo, "sub"), 0o755))
		t.Chdir(filepath.Join(repo, "sub"))
		results, err := f.results("skill", "install", "--project")
		require.NoError(t, err)
		require.Equal(t, []skillResult{
			{Path: filepath.Join(repo, ".agents", "skills", "kagent"), Status: "added"},
			{Path: filepath.Join(repo, ".claude", "skills", "kagent"), Status: "linked"},
		}, results)
		require.NoDirExists(t, f.dir())
	})
}

func TestSkillStatus(t *testing.T) {
	t.Run("Given an installed skill, when status runs, assert that every path is current", func(t *testing.T) {
		f := newSkillFixture(t).withClaude()
		_, err := f.results("skill", "install")
		require.NoError(t, err)
		results, err := f.results("skill", "status")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: f.dir(), Status: "current"}, {Path: f.link(), Status: "current"}}, results)
	})

	scenarios := []struct {
		change string
		apply  func(f *skillFixture)
	}{
		{change: "an edited file", apply: func(f *skillFixture) { f.write(f.dir(), "SKILL.md", "---\nname: kagent\n---\nedited\n") }},
		{change: "an extra file", apply: func(f *skillFixture) { f.write(f.dir(), "notes.md", "extra\n") }},
		{change: "a deleted file", apply: func(f *skillFixture) {
			require.NoError(f.t, os.Remove(filepath.Join(f.dir(), "references", "sandboxes.md")))
		}},
	}
	for _, scenario := range scenarios {
		t.Run(fmt.Sprintf("Given an installed skill with %s, when status runs, assert that every path is outdated and status fails", scenario.change), func(t *testing.T) {
			f := newSkillFixture(t).withClaude()
			_, err := f.results("skill", "install")
			require.NoError(t, err)
			scenario.apply(f)
			results, err := f.results("skill", "status")
			require.ErrorContains(t, err, "run kagent skill install")
			require.Equal(t, []skillResult{{Path: f.dir(), Status: "outdated"}, {Path: f.link(), Status: "outdated"}}, results)
		})
	}

	t.Run("Given no installed skill, when status runs, assert that every path is missing and status fails", func(t *testing.T) {
		f := newSkillFixture(t).withClaude()
		results, err := f.results("skill", "status")
		require.ErrorContains(t, err, "run kagent skill install")
		require.Equal(t, []skillResult{{Path: f.dir(), Status: "missing"}, {Path: f.link(), Status: "missing"}}, results)
	})
}

func TestSkillUninstall(t *testing.T) {
	t.Run("Given an installed skill, when uninstall runs, assert that the link and the directory are removed", func(t *testing.T) {
		f := newSkillFixture(t).withClaude()
		_, err := f.results("skill", "install")
		require.NoError(t, err)
		results, err := f.results("skill", "uninstall")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: f.link(), Status: "removed"}, {Path: f.dir(), Status: "removed"}}, results)
		_, err = os.Lstat(f.link())
		require.ErrorIs(t, err, os.ErrNotExist)
		require.NoDirExists(t, f.dir())
		require.DirExists(t, filepath.Dir(f.link()))
	})

	t.Run("Given no installed skill, when uninstall runs, assert that it succeeds and reports both paths absent", func(t *testing.T) {
		f := newSkillFixture(t).withClaude()
		results, err := f.results("skill", "uninstall")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: f.link(), Status: "absent"}, {Path: f.dir(), Status: "absent"}}, results)
	})

	t.Run("Given a directory holding a different skill, when uninstall runs, assert that it refuses and keeps the directory", func(t *testing.T) {
		f := newSkillFixture(t)
		f.write(f.dir(), "SKILL.md", "---\nname: other\n---\n")
		_, err := f.results("skill", "uninstall")
		require.ErrorContains(t, err, "holds a different skill")
		require.FileExists(t, filepath.Join(f.dir(), "SKILL.md"))
	})
}

func TestSkillGitGuard(t *testing.T) {
	t.Run("Given a committed project skill that differs from the binary, when install --project runs, assert that it updates the copy", func(t *testing.T) {
		f := newSkillFixture(t)
		dir := f.committedProjectSkill("older")
		results, err := f.results("skill", "install", "--project")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: dir, Status: "updated"}}, results)
		require.Equal(t, f.skill, f.read(dir))
	})

	t.Run("Given a project skill with uncommitted edits, when install --project runs, assert that it refuses and keeps the edits", func(t *testing.T) {
		f := newSkillFixture(t)
		dir := f.committedProjectSkill("committed")
		f.write(dir, "SKILL.md", "---\nname: kagent\n---\nunsaved\n")
		_, err := f.results("skill", "install", "--project")
		require.ErrorContains(t, err, "uncommitted changes")
		require.Equal(t, map[string]string{"SKILL.md": "---\nname: kagent\n---\nunsaved\n"}, f.read(dir))
	})

	t.Run("Given a project skill with uncommitted edits, when install --project --force runs, assert that the embedded skill replaces it", func(t *testing.T) {
		f := newSkillFixture(t)
		dir := f.committedProjectSkill("committed")
		f.write(dir, "SKILL.md", "---\nname: kagent\n---\nunsaved\n")
		results, err := f.results("skill", "install", "--project", "--force")
		require.NoError(t, err)
		require.Equal(t, []skillResult{{Path: dir, Status: "updated"}}, results)
		require.Equal(t, f.skill, f.read(dir))
	})

	t.Run("Given a project skill with uncommitted edits and a Claude Code link, when uninstall --project runs, assert that it refuses and removes nothing", func(t *testing.T) {
		f := newSkillFixture(t).withClaude()
		dir := f.committedProjectSkill("committed")
		link := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(dir))), ".claude", "skills", "kagent")
		require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
		require.NoError(t, os.Symlink(filepath.Join("..", "..", ".agents", "skills", "kagent"), link))
		f.write(dir, "SKILL.md", "---\nname: kagent\n---\nunsaved\n")
		_, err := f.results("skill", "uninstall", "--project")
		require.ErrorContains(t, err, "uncommitted changes")
		require.FileExists(t, filepath.Join(dir, "SKILL.md"))
		_, err = os.Lstat(link)
		require.NoError(t, err, "the Claude Code link must survive a refused uninstall")
	})
}

func TestEmbeddedSkillMatchesRepository(t *testing.T) {
	t.Run("Given the kagent skill in .agents/skills, when the binary embeds it, assert that the embedded copy is identical", func(t *testing.T) {
		repo, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
		require.NoError(t, err)
		source, err := readSkill(os.DirFS(filepath.Join(repo, ".agents", "skills", "kagent")))
		require.NoError(t, err)
		embedded, err := embeddedSkill()
		require.NoError(t, err)
		require.Equal(t, skillStrings(source), skillStrings(embedded),
			"the embedded skill is stale; run go generate ./core/cli/internal/commands")
	})
}

func TestSkillManifestExamplesMatchTypes(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	require.NoError(t, err)
	skill := os.DirFS(filepath.Join(repo, ".agents", "skills", "kagent"))
	references, err := fs.Glob(skill, "references/*.md")
	require.NoError(t, err)
	var blocks []skillYAMLBlock
	for _, name := range append([]string{"SKILL.md"}, references...) {
		data, err := fs.ReadFile(skill, name)
		require.NoError(t, err)
		blocks = append(blocks, skillYAMLBlocks(name, string(data))...)
	}
	require.NotEmpty(t, blocks, "the kagent skill must show at least one manifest")
	for _, block := range blocks {
		t.Run(fmt.Sprintf("Given the yaml block at %s, when kagent apply parses it, assert that it decodes strictly", block.where), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "manifest.yaml")
			require.NoError(t, os.WriteFile(file, []byte(block.text), 0o600))
			var kind struct {
				Kind string `json:"kind"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(block.text), &kind), block.where)
			switch kind.Kind {
			case agentKind:
				_, _, err = readAgentManifest(file, "kagent")
			case agentTemplateKind:
				_, _, err = readAgentTemplateManifest(file, "kagent")
			case "ModelConfig":
				// kubectl applies ModelConfig, so a strict decode stands in for the API server's unknown-field check.
				err = yaml.UnmarshalStrict([]byte(block.text), &apiv1alpha3.ModelConfig{})
			default:
				t.Fatalf("%s: kind %q is not Agent, AgentTemplate, or ModelConfig; show full resources so this test can parse them", block.where, kind.Kind)
			}
			require.NoError(t, err, block.where)
		})
	}
}

type skillYAMLBlock struct {
	where string
	text  string
}

// skillYAMLBlocks returns each ```yaml block in a Markdown file, named by its line and heading.
func skillYAMLBlocks(name, markdown string) []skillYAMLBlock {
	var blocks []skillYAMLBlock
	var heading, indent string
	var current *strings.Builder
	start, otherFence := 0, false
	for i, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case current != nil && trimmed == "```":
			blocks = append(blocks, skillYAMLBlock{where: fmt.Sprintf("%s:%d (%s)", name, start, heading), text: current.String()})
			current = nil
		case current != nil:
			current.WriteString(strings.TrimPrefix(line, indent) + "\n")
		case !otherFence && trimmed == "```yaml":
			current, start = &strings.Builder{}, i+1
			indent = line[:len(line)-len(strings.TrimLeft(line, " "))]
		case strings.HasPrefix(trimmed, "```"):
			otherFence = !otherFence
		case !otherFence && strings.HasPrefix(line, "#"):
			heading = trimmed
		}
	}
	return blocks
}

// skillFixture runs kagent skill commands in an empty home directory.
type skillFixture struct {
	t     *testing.T
	home  string
	skill map[string]string
}

func newSkillFixture(t *testing.T) *skillFixture {
	embedded, err := embeddedSkill()
	require.NoError(t, err)
	f := &skillFixture{t: t, home: t.TempDir(), skill: skillStrings(embedded)}
	t.Setenv("HOME", f.home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return f
}

func (f *skillFixture) withClaude() *skillFixture {
	require.NoError(f.t, os.Mkdir(filepath.Join(f.home, ".claude"), 0o755))
	return f
}

// committedProjectSkill makes a git repository, switches into it, and commits a kagent skill with body there.
func (f *skillFixture) committedProjectSkill(body string) string {
	repo, err := filepath.EvalSymlinks(f.t.TempDir())
	require.NoError(f.t, err)
	dir := filepath.Join(repo, ".agents", "skills", "kagent")
	f.write(dir, "SKILL.md", "---\nname: kagent\n---\n"+body+"\n")
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "."}, {"commit", "--quiet", "-m", "add the kagent skill"}} {
		git := append([]string{"-C", repo, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)
		out, err := exec.Command("git", git...).CombinedOutput()
		require.NoError(f.t, err, string(out))
	}
	f.t.Chdir(repo)
	return dir
}

func (f *skillFixture) dir() string  { return filepath.Join(f.home, ".agents", "skills", "kagent") }
func (f *skillFixture) link() string { return filepath.Join(f.home, ".claude", "skills", "kagent") }

func (f *skillFixture) run(args ...string) (string, string, error) {
	root := &cobra.Command{Use: "kagent", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().StringP(clioutput.FlagName, "o", string(clioutput.FormatTable), "Output format")
	root.AddCommand(NewSkillCmd())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(f.t.Context())
	return stdout.String(), stderr.String(), err
}

// results runs a command with JSON output and decodes its result rows.
func (f *skillFixture) results(args ...string) ([]skillResult, error) {
	stdout, _, err := f.run(append([]string{"-o", "json"}, args...)...)
	var results []skillResult
	if stdout != "" {
		require.NoError(f.t, json.Unmarshal([]byte(stdout), &results))
	}
	return results, err
}

func (f *skillFixture) write(dir, name, content string) {
	path := filepath.Join(dir, filepath.FromSlash(name))
	require.NoError(f.t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(f.t, os.WriteFile(path, []byte(content), 0o644))
}

func (f *skillFixture) read(dir string) map[string]string {
	files, err := readSkill(os.DirFS(dir))
	require.NoError(f.t, err)
	return skillStrings(files)
}

func skillStrings(files map[string][]byte) map[string]string {
	contents := map[string]string{}
	for name, data := range files {
		contents[name] = string(data)
	}
	return contents
}
