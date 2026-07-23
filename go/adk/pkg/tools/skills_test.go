package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	skillruntime "github.com/kagent-dev/kagent/go/adk/pkg/skills"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// fakeToolContext is a minimal agent.Context for directly invoking tools in
// tests, bypassing the full ADK flow engine. Embeds StrictContextMock (an
// ADK test double) and overrides only the methods functiontool.Run() calls.
type fakeToolContext struct {
	adkagent.StrictContextMock
	sessionID string
}

func (f *fakeToolContext) SessionID() string { return f.sessionID }
func (f *fakeToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return nil
}

// runnableTool mirrors the unexported Run method that functiontool.New's
// concrete type implements; declaring it locally lets us type-assert without
// depending on functiontool internals.
type runnableTool interface {
	Run(ctx adkagent.Context, args any) (map[string]any, error)
}

func runTool(t *testing.T, tl tool.Tool, ctx adkagent.Context, args map[string]any) string {
	t.Helper()
	runner, ok := tl.(runnableTool)
	if !ok {
		t.Fatalf("tool %q does not support direct invocation", tl.Name())
	}
	result, err := runner.Run(ctx, args)
	if err != nil {
		t.Fatalf("%s.Run() error = %v", tl.Name(), err)
	}
	text, ok := result["result"].(string)
	if !ok {
		t.Fatalf("%s.Run() result = %#v, want map with string \"result\"", tl.Name(), result)
	}
	return text
}

func TestResolveReadPath_AllowsSymlinkedSkillsDirectory(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	skillsDir := t.TempDir()
	skillFile := filepath.Join(skillsDir, "script.py")
	if err := os.WriteFile(skillFile, []byte("print('ok')\n"), 0644); err != nil {
		t.Fatalf("failed to write skill file: %v", err)
	}

	sessionID := fmt.Sprintf("%s-read", t.Name())
	resolved, err := resolveReadPath(sessionID, skillsDir, "skills/script.py")
	if err != nil {
		t.Fatalf("resolveReadPath() error = %v", err)
	}
	want, err := filepath.EvalSymlinks(skillFile)
	if err != nil {
		t.Fatalf("EvalSymlinks(skillFile) error = %v", err)
	}
	if resolved != want {
		t.Fatalf("resolveReadPath() = %q, want %q", resolved, want)
	}
}

func TestResolveWritePath_BlocksSkillsSymlink(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	skillsDir := t.TempDir()
	sessionID := fmt.Sprintf("%s-write", t.Name())
	_, err := resolveWritePath(sessionID, skillsDir, "skills/new-file.txt")
	if err == nil {
		t.Fatal("expected write through skills symlink to be rejected")
	}
	if !strings.Contains(err.Error(), "outside the writable session directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewSkillsTools_ReturnsExpectedToolSet(t *testing.T) {
	skillsDir := t.TempDir()
	t.Setenv("KAGENT_SRT_SETTINGS_PATH", filepath.Join(t.TempDir(), "srt-settings.json"))
	t.Setenv("KAGENT_ENABLE_FILE_SEARCH_TOOLS", "true")
	skillDir := filepath.Join(skillsDir, "demo")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: demo
description: Demo skill.
---
`), 0644); err != nil {
		t.Fatalf("failed to write skill metadata: %v", err)
	}

	tools, err := NewSkillsTools(skillsDir)
	if err != nil {
		t.Fatalf("NewSkillsTools() error = %v", err)
	}

	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.Name()] = true
	}

	for _, name := range []string{"skills", "read_file", "write_file", "edit_file", "list_files", "grep_file", "bash"} {
		if !got[name] {
			t.Errorf("expected tool %q to be present", name)
		}
	}
}

func TestNewSkillsTools_OmitsBashWithoutSRTSettings(t *testing.T) {
	skillsDir := t.TempDir()
	t.Setenv("KAGENT_SRT_SETTINGS_PATH", "")
	t.Setenv("KAGENT_ENABLE_FILE_SEARCH_TOOLS", "true")

	tools, err := NewSkillsTools(skillsDir)
	if err != nil {
		t.Fatalf("NewSkillsTools() error = %v, want nil (bash should be omitted, not fatal)", err)
	}

	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.Name()] = true
	}

	for _, name := range []string{"skills", "read_file", "write_file", "edit_file", "list_files", "grep_file"} {
		if !got[name] {
			t.Errorf("expected tool %q to be present even without SRT settings", name)
		}
	}
	if got["bash"] {
		t.Error("expected bash tool to be omitted without SRT settings")
	}
}

func TestNewSkillsTools_OmitsListFilesAndGrepFileByDefault(t *testing.T) {
	skillsDir := t.TempDir()
	t.Setenv("KAGENT_SRT_SETTINGS_PATH", filepath.Join(t.TempDir(), "srt-settings.json"))
	t.Setenv("KAGENT_ENABLE_FILE_SEARCH_TOOLS", "")

	tools, err := NewSkillsTools(skillsDir)
	if err != nil {
		t.Fatalf("NewSkillsTools() error = %v, want nil (list_files/grep_file should be omitted, not fatal)", err)
	}

	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.Name()] = true
	}

	for _, name := range []string{"skills", "read_file", "write_file", "edit_file", "bash"} {
		if !got[name] {
			t.Errorf("expected tool %q to be present even without KAGENT_ENABLE_FILE_SEARCH_TOOLS", name)
		}
	}
	if got["list_files"] {
		t.Error("expected list_files tool to be omitted by default")
	}
	if got["grep_file"] {
		t.Error("expected grep_file tool to be omitted by default")
	}
}

func TestNewSkillsTools_BashDescriptionMentionsFileSearchToolsOnlyWhenEnabled(t *testing.T) {
	skillsDir := t.TempDir()
	t.Setenv("KAGENT_SRT_SETTINGS_PATH", filepath.Join(t.TempDir(), "srt-settings.json"))

	findBash := func(t *testing.T, tools []tool.Tool) tool.Tool {
		t.Helper()
		for _, tl := range tools {
			if tl.Name() == "bash" {
				return tl
			}
		}
		t.Fatal("expected bash tool to be present")
		return nil
	}

	t.Run("disabled by default", func(t *testing.T) {
		t.Setenv("KAGENT_ENABLE_FILE_SEARCH_TOOLS", "")
		tools, err := NewSkillsTools(skillsDir)
		if err != nil {
			t.Fatalf("NewSkillsTools() error = %v", err)
		}
		desc := findBash(t, tools).Description()
		if strings.Contains(desc, "list_files") || strings.Contains(desc, "grep_file") {
			t.Errorf("bash description should not mention list_files/grep_file when disabled, got %q", desc)
		}
	})

	t.Run("mentioned when enabled", func(t *testing.T) {
		t.Setenv("KAGENT_ENABLE_FILE_SEARCH_TOOLS", "true")
		tools, err := NewSkillsTools(skillsDir)
		if err != nil {
			t.Fatalf("NewSkillsTools() error = %v", err)
		}
		desc := findBash(t, tools).Description()
		if !strings.Contains(desc, "list_files and grep_file") {
			t.Errorf("bash description should mention list_files and grep_file when enabled, got %q", desc)
		}
	})
}

// TestListFilesAndGrepFileTools_RunThroughADK invokes the real functiontool.Run()
// path (the same one the ADK flow engine uses to execute a model's tool call),
// rather than calling ListDirContent/GrepContent directly, to verify the
// closures in NewSkillsTools correctly wire path resolution and argument
// parsing end-to-end.
func TestListFilesAndGrepFileTools_RunThroughADK(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	skillsDir := t.TempDir()
	t.Setenv("KAGENT_SRT_SETTINGS_PATH", "")
	t.Setenv("KAGENT_ENABLE_FILE_SEARCH_TOOLS", "true")

	tools, err := NewSkillsTools(skillsDir)
	if err != nil {
		t.Fatalf("NewSkillsTools() error = %v", err)
	}

	var listFilesTool, grepFileTool tool.Tool
	for _, tl := range tools {
		switch tl.Name() {
		case "list_files":
			listFilesTool = tl
		case "grep_file":
			grepFileTool = tl
		}
	}
	if listFilesTool == nil || grepFileTool == nil {
		t.Fatal("expected list_files and grep_file tools to be present")
	}

	sessionID := fmt.Sprintf("%s-session", t.Name())
	sessionPath, err := skillruntime.GetSessionPath(sessionID, skillsDir)
	if err != nil {
		t.Fatalf("GetSessionPath() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionPath, "notes.txt"), []byte("hello kagent\nsecond line\n"), 0644); err != nil {
		t.Fatalf("failed to seed session file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(sessionPath, "logs"), 0755); err != nil {
		t.Fatalf("failed to create session subdir: %v", err)
	}

	ctx := &fakeToolContext{sessionID: sessionID}

	t.Run("list_files defaults to the working directory", func(t *testing.T) {
		result := runTool(t, listFilesTool, ctx, map[string]any{})
		if !strings.Contains(result, "notes.txt") || !strings.Contains(result, "logs/") {
			t.Errorf("list_files result = %q, want entries for notes.txt and logs/", result)
		}
	})

	t.Run("grep_file finds a match by relative path", func(t *testing.T) {
		result := runTool(t, grepFileTool, ctx, map[string]any{
			"pattern": "kagent",
			"path":    "notes.txt",
		})
		if !strings.Contains(result, "notes.txt:1:hello kagent") {
			t.Errorf("grep_file result = %q, want a match on line 1", result)
		}
	})

	t.Run("grep_file reports no matches without erroring", func(t *testing.T) {
		result := runTool(t, grepFileTool, ctx, map[string]any{
			"pattern": "nope",
			"path":    "notes.txt",
		})
		if result != "no matches found" {
			t.Errorf("grep_file result = %q, want %q", result, "no matches found")
		}
	})

	t.Run("list_files rejects paths outside the session/skills roots", func(t *testing.T) {
		result := runTool(t, listFilesTool, ctx, map[string]any{"path": "/etc"})
		if !strings.Contains(result, "outside the allowed roots") {
			t.Errorf("list_files result = %q, want an outside-allowed-roots error", result)
		}
	})
}
