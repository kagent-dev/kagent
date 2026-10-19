package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestResolveToolPath(t *testing.T) {
	workspace := t.TempDir()
	for _, tt := range []struct{ name, input, want string }{
		{"relative", "notes.txt", filepath.Join(workspace, "notes.txt")},
		{"absolute", "/etc/hosts", "/etc/hosts"},
		{"parent", "../sibling", filepath.Join(workspace, "../sibling")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveToolPath(workspace, tt.input)
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestNewBuiltinTools_ReturnsExpectedToolSet(t *testing.T) {
	skillsDir := t.TempDir()
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

	tools, err := NewBuiltinTools(BuiltinToolsConfig{Workspace: t.TempDir(), SkillsDirectory: skillsDir})
	if err != nil {
		t.Fatalf("NewBuiltinTools() error = %v", err)
	}

	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.Name()] = true
	}

	for _, name := range []string{"read_file", "write_file", "edit_file", "list_files", "grep_file", "bash"} {
		if !got[name] {
			t.Errorf("expected tool %q to be present", name)
		}
	}
}

func TestNewBuiltinTools_DisableFileSearchOmitsListFilesAndGrepFile(t *testing.T) {
	skillsDir := t.TempDir()

	tools, err := NewBuiltinTools(BuiltinToolsConfig{Workspace: t.TempDir(), SkillsDirectory: skillsDir, DisableFileSearch: true})
	if err != nil {
		t.Fatalf("NewBuiltinTools() error = %v, want nil (list_files/grep_file should be omitted, not fatal)", err)
	}

	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.Name()] = true
	}

	for _, name := range []string{"read_file", "write_file", "edit_file", "bash"} {
		if !got[name] {
			t.Errorf("expected tool %q to be present when file search tools are disabled", name)
		}
	}
	if got["list_files"] {
		t.Error("expected list_files tool to be omitted by default")
	}
	if got["grep_file"] {
		t.Error("expected grep_file tool to be omitted by default")
	}
}

func TestNewBuiltinTools_DisableBashPreservesFileTools(t *testing.T) {
	for _, fileSearchEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("file_search=%t", fileSearchEnabled), func(t *testing.T) {
			tools, err := NewBuiltinTools(BuiltinToolsConfig{Workspace: t.TempDir(), DisableFileSearch: !fileSearchEnabled, DisableBash: true})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, tool := range tools {
				got[tool.Name()] = true
			}
			for _, name := range []string{"read_file", "write_file", "edit_file"} {
				if !got[name] {
					t.Errorf("expected tool %q to remain available", name)
				}
			}
			if got["bash"] {
				t.Error("expected bash to be omitted")
			}
			for _, name := range []string{"list_files", "grep_file"} {
				if got[name] != fileSearchEnabled {
					t.Errorf("tool %q present = %t, want %t", name, got[name], fileSearchEnabled)
				}
			}
		})
	}
}

func TestNewBuiltinTools_BashDescriptionMentionsFileSearchToolsOnlyWhenEnabled(t *testing.T) {
	skillsDir := t.TempDir()

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

	t.Run("explicitly disabled", func(t *testing.T) {
		tools, err := NewBuiltinTools(BuiltinToolsConfig{Workspace: t.TempDir(), SkillsDirectory: skillsDir, DisableFileSearch: true})
		if err != nil {
			t.Fatalf("NewBuiltinTools() error = %v", err)
		}
		desc := findBash(t, tools).Description()
		if strings.Contains(desc, "list_files") || strings.Contains(desc, "grep_file") {
			t.Errorf("bash description should not mention list_files/grep_file when disabled, got %q", desc)
		}
	})

	t.Run("mentioned when enabled", func(t *testing.T) {
		tools, err := NewBuiltinTools(BuiltinToolsConfig{Workspace: t.TempDir(), SkillsDirectory: skillsDir})
		if err != nil {
			t.Fatalf("NewBuiltinTools() error = %v", err)
		}
		desc := findBash(t, tools).Description()
		if !strings.Contains(desc, "list_files and grep_file") {
			t.Errorf("bash description should mention list_files and grep_file when enabled, got %q", desc)
		}
	})
}

// TestListFilesAndGrepFileTools_RunThroughADK invokes the real functiontool.Run()
// path (the same one the ADK flow engine uses to execute a model's tool call),
// rather than calling listDirContent/grepContent directly, to verify the
// closures in NewBuiltinTools correctly wire path resolution and
// argument parsing end-to-end.
func TestListFilesAndGrepFileTools_RunThroughADK(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	skillsDir := t.TempDir()
	sessionPath := t.TempDir()

	tools, err := NewBuiltinTools(BuiltinToolsConfig{Workspace: sessionPath, SkillsDirectory: skillsDir})
	if err != nil {
		t.Fatalf("NewBuiltinTools() error = %v", err)
	}

	var listFilesTool, grepFileTool, readFileTool, bashTool tool.Tool
	for _, tl := range tools {
		switch tl.Name() {
		case "list_files":
			listFilesTool = tl
		case "grep_file":
			grepFileTool = tl
		case "read_file":
			readFileTool = tl
		case "bash":
			bashTool = tl
		}
	}
	if listFilesTool == nil || grepFileTool == nil || readFileTool == nil || bashTool == nil {
		t.Fatal("expected list_files, grep_file, read_file and bash tools to be present")
	}

	sessionID := fmt.Sprintf("%s-session", t.Name())
	if err := os.WriteFile(filepath.Join(sessionPath, "notes.txt"), []byte("hello kagent\nsecond line\n"), 0644); err != nil {
		t.Fatalf("failed to seed session file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(sessionPath, "logs"), 0755); err != nil {
		t.Fatalf("failed to create session subdir: %v", err)
	}

	ctx := &fakeToolContext{StrictContextMock: adkagent.NewStrictContextMock(t.Context()), sessionID: sessionID}

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

	t.Run("list_files accepts absolute sandbox paths", func(t *testing.T) {
		absolute := t.TempDir()
		if err := os.WriteFile(filepath.Join(absolute, "absolute.txt"), []byte("ok"), 0600); err != nil {
			t.Fatal(err)
		}
		result := runTool(t, listFilesTool, ctx, map[string]any{"path": absolute})
		if !strings.Contains(result, "absolute.txt") {
			t.Fatal(result)
		}
	})

	// Exercised here, through the registered tool rather than against
	// readFileContent directly, because the failure this guards was invisible
	// at the function level: read_file and grep_file scanned with different
	// buffer limits, so a file grep_file handled fine made read_file fail
	// outright. Driving the same tree through both tools is what makes that
	// asymmetry observable.
	t.Run("read_file and grep_file agree on a line past bufio's default", func(t *testing.T) {
		longLine := strings.Repeat("a", 100_000)
		bundle := filepath.Join(sessionPath, "bundle.min.js")
		if err := os.WriteFile(bundle, []byte(longLine+"\nneedle here\n"), 0644); err != nil {
			t.Fatalf("failed to seed long-line file: %v", err)
		}

		read := runTool(t, readFileTool, ctx, map[string]any{"file_path": "bundle.min.js"})
		if strings.Contains(read, "Error reading file") {
			t.Fatalf("read_file failed on a long line instead of truncating: %q", read)
		}
		readLines := strings.Split(read, "\n")
		if len(readLines) != 2 {
			t.Fatalf("read_file returned %d lines, want 2: %.200q", len(readLines), read)
		}
		body := strings.TrimSuffix(strings.SplitN(readLines[0], "|", 2)[1], "...")
		if n := utf8.RuneCountInString(body); n != maxLineRunes {
			t.Errorf("read_file truncated line 1 to %d runes, want %d", n, maxLineRunes)
		}
		if !strings.Contains(readLines[1], "needle here") {
			t.Errorf("read_file lost the line after the long one: %q", readLines[1])
		}

		// The same file must remain greppable -- this is the side that always
		// worked, and it is what read_file is now consistent with.
		grep := runTool(t, grepFileTool, ctx, map[string]any{
			"pattern": "needle here",
			"path":    "bundle.min.js",
		})
		if !strings.Contains(grep, "bundle.min.js:2:needle here") {
			t.Errorf("grep_file result = %q, want the match on line 2", grep)
		}
	})

	t.Run("list_files reports a symlink's target size, not the link's", func(t *testing.T) {
		linkDir := filepath.Join(sessionPath, "linkdir")
		if err := os.Mkdir(linkDir, 0755); err != nil {
			t.Fatalf("failed to create linkdir: %v", err)
		}
		target := filepath.Join(linkDir, "target.txt")
		if err := os.WriteFile(target, []byte(strings.Repeat("x", 5000)), 0644); err != nil {
			t.Fatalf("failed to write symlink target: %v", err)
		}
		// A relative target, so the stored path string is far shorter than the
		// file -- an Lstat-based size would report 10, not 5000.
		if err := os.Symlink("target.txt", filepath.Join(linkDir, "file-link")); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}

		result := runTool(t, listFilesTool, ctx, map[string]any{"path": "linkdir"})
		if !strings.Contains(result, "file-link\t5000") {
			t.Errorf("list_files result = %q, want file-link sized by its target (5000)", result)
		}
	})
}

// Exercise file and shell calls in a workspace that can be reopened after restore.
func TestBuiltinToolsWithoutSkills(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	workspace := t.TempDir()
	builtinTools, err := NewBuiltinTools(BuiltinToolsConfig{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]tool.Tool)
	for _, tl := range builtinTools {
		byName[tl.Name()] = tl
	}
	ctx := &fakeToolContext{StrictContextMock: adkagent.NewStrictContextMock(t.Context()), sessionID: "no-skills"}
	if got := runTool(t, byName["write_file"], ctx, map[string]any{"file_path": "notes.txt", "content": "hello"}); strings.Contains(got, "Error") {
		t.Fatal(got)
	}
	if got := runTool(t, byName["read_file"], ctx, map[string]any{"file_path": "notes.txt"}); !strings.Contains(got, "hello") {
		t.Fatal(got)
	}
	if got := runTool(t, byName["edit_file"], ctx, map[string]any{"file_path": "notes.txt", "old_string": "hello", "new_string": "updated"}); strings.Contains(got, "Error") {
		t.Fatal(got)
	}
	if got := runTool(t, byName["bash"], ctx, map[string]any{"command": "cat notes.txt"}); got != "updated" {
		t.Fatal(got)
	}
	if got := runTool(t, byName["list_files"], ctx, map[string]any{}); !strings.Contains(got, "notes.txt") {
		t.Fatal(got)
	}
	if got := runTool(t, byName["grep_file"], ctx, map[string]any{"path": "notes.txt", "pattern": "updated"}); !strings.Contains(got, "updated") {
		t.Fatal(got)
	}
	// Reconstruct the tools over the same durable directory, as after Actor replacement.
	restored, err := NewBuiltinTools(BuiltinToolsConfig{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range restored {
		if tl.Name() == "read_file" {
			got := runTool(t, tl, ctx, map[string]any{"file_path": "notes.txt"})
			if !strings.Contains(got, "updated") {
				t.Fatal(got)
			}
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	got := runTool(t, byName["write_file"], ctx, map[string]any{"file_path": outside, "content": "absolute"})
	if strings.Contains(got, "Error") {
		t.Fatal(got)
	}
	if got := runTool(t, byName["read_file"], ctx, map[string]any{"file_path": outside}); !strings.Contains(got, "absolute") {
		t.Fatal(got)
	}
}
