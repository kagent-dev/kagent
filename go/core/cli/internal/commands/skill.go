package commands

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/kagent-dev/kagent/go/core/cli/internal/common/prompt"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

const (
	skillName = "kagent"
	skillsDir = "skills"
	// agentsDir holds the skills that most skills-compatible AI agents read. Claude Code reads only claudeDir.
	agentsDir = ".agents"
	claudeDir = ".claude"

	statusCurrent   = "current"
	statusOutdated  = "outdated"
	statusMissing   = "missing"
	statusUnchanged = "unchanged"
)

// skillData is the kagent skill. go:embed cannot read outside the go/ module,
// so the files live here and .agents/skills/kagent links to them.
//
//go:embed skilldata/kagent
var skillData embed.FS

type skillResult struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// NewSkillCmd constructs the kagent skill command tree.
func NewSkillCmd() *cobra.Command {
	var project, force, yes bool
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install, check, or remove the kagent Agent Skill",
	}
	cmd.PersistentFlags().BoolVar(&project, "project", false, "Use the git top level instead of the home directory")

	install := &cobra.Command{
		Use:   "install",
		Short: "Install or update the kagent skill",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, paths, err := skillTargets(cmd, project)
			if err != nil {
				return err
			}
			if !yes {
				if err := confirmSkillInstall(cmd.InOrStdin(), cmd.ErrOrStderr(), paths); err != nil {
					return err
				}
			}
			files, err := embeddedSkill()
			if err != nil {
				return err
			}
			results, err := installSkill(paths, files, force)
			if err != nil {
				return err
			}
			return writeSkillResults(cmd.OutOrStdout(), format, results)
		},
	}
	install.Flags().BoolVar(&force, "force", false, "Replace a directory that holds a different skill")
	install.Flags().BoolVarP(&yes, "yes", "y", false, "Install without asking for confirmation")

	status := &cobra.Command{
		Use:   "status",
		Short: "Check that the installed kagent skill matches this binary",
		Long:  "Compare each installed copy with the skill in this binary. Exit 1 when one is missing or outdated.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, paths, err := skillTargets(cmd, project)
			if err != nil {
				return err
			}
			files, err := embeddedSkill()
			if err != nil {
				return err
			}
			var results []skillResult
			stale := false
			for _, path := range paths {
				status, err := skillStatus(path, files)
				if err != nil {
					return err
				}
				results = append(results, skillResult{Path: path, Status: status})
				stale = stale || status != statusCurrent
			}
			if err := writeSkillResults(cmd.OutOrStdout(), format, results); err != nil {
				return err
			}
			if stale {
				return fmt.Errorf("the kagent skill is missing or outdated; run kagent skill install%s", projectFlag(project))
			}
			return nil
		},
	}

	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the kagent skill and its Claude Code link",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, paths, err := skillTargets(cmd, project)
			if err != nil {
				return err
			}
			for _, path := range paths {
				if err := checkRemovable(path); err != nil {
					return err
				}
			}
			var results []skillResult
			// Remove the Claude Code link first, so it never dangles.
			for _, path := range slices.Backward(paths) {
				status, err := removeSkill(path)
				if err != nil {
					return err
				}
				results = append(results, skillResult{Path: path, Status: status})
			}
			return writeSkillResults(cmd.OutOrStdout(), format, results)
		},
	}

	cmd.AddCommand(install, status, uninstall)
	return cmd
}

// skillTargets returns the output format and the skill paths that every skill command needs.
func skillTargets(cmd *cobra.Command, project bool) (clioutput.Format, []string, error) {
	value, err := clioutput.FromCommand(cmd)
	if err != nil {
		return "", nil, err
	}
	format, err := clioutput.Parse(value)
	if err != nil {
		return "", nil, err
	}
	paths, err := skillPaths(cmd.Context(), project)
	return format, paths, err
}

func projectFlag(project bool) string {
	if project {
		return " --project"
	}
	return ""
}

// confirmSkillInstall lists the target paths and asks once, only when a person is at a terminal.
func confirmSkillInstall(in io.Reader, out io.Writer, paths []string) error {
	if !prompt.IsTerminal(in) || !prompt.IsTerminal(out) {
		return nil
	}
	_, _ = fmt.Fprintln(out, "kagent will install its Agent Skill to:")
	_, _ = fmt.Fprintf(out, "  %s (read by most skills-compatible AI agents)\n", paths[0])
	for _, link := range paths[1:] {
		_, _ = fmt.Fprintf(out, "  %s (Claude Code, links to the folder above)\n", link)
	}
	ok, err := prompt.Confirm(in, out, "Continue?")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("skill install cancelled; nothing was written")
	}
	return nil
}

// skillPaths returns the skill directory, then the Claude Code link when Claude Code is installed.
func skillPaths(ctx context.Context, project bool) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	claude := env.ClaudeConfigDir.Get()
	hasClaude := claude != ""
	if !hasClaude {
		claude = filepath.Join(home, claudeDir)
		_, err := os.Stat(claude)
		hasClaude = err == nil
	}
	root := home
	if project {
		out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			return nil, fmt.Errorf("--project needs a git repository: %w", err)
		}
		root = strings.TrimSpace(string(out))
		claude = filepath.Join(root, claudeDir)
	}
	dirs := []string{filepath.Join(root, agentsDir)}
	if hasClaude {
		dirs = append(dirs, claude)
	}
	paths := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		paths = append(paths, filepath.Join(dir, skillsDir, skillName))
	}
	return paths, nil
}

// embeddedSkill returns the skill files built into this binary.
func embeddedSkill() (map[string][]byte, error) {
	fsys, err := fs.Sub(skillData, "skilldata/kagent")
	if err != nil {
		return nil, err
	}
	return readSkill(fsys)
}

// installSkill writes files to the first path and links the rest to it.
func installSkill(paths []string, files map[string][]byte, force bool) ([]skillResult, error) {
	status, err := writeSkill(paths[0], files, force)
	if err != nil {
		return nil, err
	}
	results := []skillResult{{Path: paths[0], Status: status}}
	for _, link := range paths[1:] {
		status, err := linkSkill(link, paths[0], files, force)
		if err != nil {
			return nil, err
		}
		results = append(results, skillResult{Path: link, Status: status})
	}
	return results, nil
}

// writeSkill replaces dir with files. It writes a temporary copy beside dir and
// renames it into place, so a failed write leaves dir as it was.
func writeSkill(dir string, files map[string][]byte, force bool) (_ string, err error) {
	status, err := skillStatus(dir, files)
	if err != nil {
		return "", err
	}
	if status == statusCurrent {
		return statusUnchanged, nil
	}
	if status == statusOutdated {
		if info, err := os.Lstat(dir); err == nil && info.Mode()&fs.ModeSymlink != 0 && !force {
			return "", fmt.Errorf("%s is a symlink; pass --force to replace it", dir)
		}
		if err := claimSkill(dir, force); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), "."+skillName+"-")
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(tmp)) }()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return "", err
	}
	for name, data := range files {
		path := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return "", err
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		return "", err
	}
	if status == statusMissing {
		return "added", nil
	}
	return "updated", nil
}

// linkSkill symlinks link to dir, or copies the files where symlinks fail (Windows).
func linkSkill(link, dir string, files map[string][]byte, force bool) (string, error) {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return "", err
	}
	// Resolve both parents, such as a symlinked ~/.claude, so the relative link does not dangle.
	from, err := filepath.EvalSymlinks(filepath.Dir(link))
	if err != nil {
		return "", err
	}
	to, err := filepath.EvalSymlinks(filepath.Dir(dir))
	if err != nil {
		return "", err
	}
	target, err := filepath.Rel(from, filepath.Join(to, filepath.Base(dir)))
	if err != nil {
		return "", err
	}
	if current, err := os.Readlink(link); err == nil && current == target {
		return statusUnchanged, nil
	}
	// A symlink holds no data, so only a real directory is claimed.
	if info, err := os.Lstat(link); err == nil && info.Mode()&fs.ModeSymlink == 0 {
		if err := claimSkill(link, force); err != nil {
			return "", err
		}
	}
	if err := os.RemoveAll(link); err != nil {
		return "", err
	}
	if err := os.Symlink(target, link); err != nil {
		return writeSkill(link, files, force)
	}
	return "linked", nil
}

// skillStatus reports missing, current, or outdated; current means exactly files.
func skillStatus(path string, files map[string][]byte) (string, error) {
	local, err := readSkill(os.DirFS(path))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return statusMissing, nil
	case err != nil:
		return "", err
	case maps.EqualFunc(local, files, bytes.Equal):
		return statusCurrent, nil
	default:
		return statusOutdated, nil
	}
}

// readSkill returns every file in fsys by slash path.
func readSkill(fsys fs.FS) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		files[path], err = fs.ReadFile(fsys, path)
		return err
	})
	return files, err
}

func claimSkill(path string, force bool) error {
	switch {
	case force:
		return nil
	case !ownsSkill(path):
		return fmt.Errorf("%s holds a different skill; pass --force to replace it", path)
	case hasUncommittedChanges(path):
		return fmt.Errorf("%s has uncommitted changes; commit them first, or pass --force", path)
	}
	return nil
}

// hasUncommittedChanges guards edits to git-tracked files, so they survive.
// Untracked files do not count, so an install that was never committed stays replaceable.
func hasUncommittedChanges(path string) bool {
	out, err := exec.Command("git", "-C", filepath.Dir(path), "status", "--porcelain", "--untracked-files=no", "--", filepath.Base(path)).Output()
	return err == nil && len(bytes.TrimSpace(out)) > 0
}

// ownsSkill reports whether the SKILL.md frontmatter in dir says name: kagent.
func ownsSkill(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return false
	}
	rest, opened := bytes.CutPrefix(data, []byte("---\n"))
	frontmatter, _, closed := bytes.Cut(rest, []byte("\n---"))
	var meta struct {
		Name string `json:"name"`
	}
	return opened && closed && yaml.Unmarshal(frontmatter, &meta) == nil && meta.Name == skillName
}

// removeSkill deletes the skill or link at path once checkRemovable has approved it.
func removeSkill(path string) (string, error) {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return "absent", nil
	}
	return "removed", os.RemoveAll(path)
}

// checkRemovable runs before any removal, so uninstall never stops halfway.
// A symlink holds no data, so it is always removable, even when it dangles.
func checkRemovable(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && info.Mode()&fs.ModeSymlink != 0) {
		return nil
	}
	if !ownsSkill(path) {
		return fmt.Errorf("%s holds a different skill; leaving it in place", path)
	}
	if hasUncommittedChanges(path) {
		return fmt.Errorf("%s has uncommitted changes; commit or discard them first", path)
	}
	return nil
}

func writeSkillResults(w io.Writer, format clioutput.Format, results []skillResult) error {
	if format == clioutput.FormatJSON {
		return clioutput.WriteJSON(w, results)
	}
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"PATH", "STATUS"})
	for _, result := range results {
		tw.AppendRow(table.Row{result.Path, result.Status})
	}
	_, err := fmt.Fprintln(w, tw.Render())
	return err
}
