package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// TestEnableFileSearchToolsEnvMatchesRegistry pins this package's env var
// literal to the `kagent env` registry entry in go/core/pkg/env. The name is
// declared twice on purpose -- the runtime reads the raw variable here, while
// the registry exists solely so `kagent env` can document it -- so without
// this assertion nothing would catch the two drifting apart.
//
// This is a test-only import: it does not put a dependency on the
// control-plane module into the shipped agent runtime.
func TestEnableFileSearchToolsEnvMatchesRegistry(t *testing.T) {
	if got, want := enableFileSearchToolsEnv, env.KagentEnableFileSearchTools.Name(); got != want {
		t.Fatalf("env var name drift: %s has %q, go/core/pkg/env registry has %q",
			"go/adk/pkg/tools/skills.go", got, want)
	}
}

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

// TestResolvePathContainment pins the sandbox boundary each resolver
// enforces. The three differ along exactly two axes -- whether the skills
// directory is an allowed root, and whether a not-yet-existing leaf is
// tolerated -- and those differences are the whole security contract:
//
//	                 session dir   skills dir   missing leaf
//	resolveReadPath   allow         allow        reject
//	resolveEditPath   allow         REJECT       reject
//	resolveWritePath  allow         REJECT       allow
//
// resolveEditPath in particular had no direct coverage before this, so a
// refactor could have silently granted it the skills root -- letting an
// agent edit read-only skill files -- with nothing failing.
func TestResolvePathContainment(t *testing.T) {
	resolvers := map[string]struct {
		fn              func(sessionID, skillsDirectory, requestedPath string) (string, error)
		allowsSkillsDir bool
		allowsNewLeaf   bool
		// deniedContains is the boundary each policy names when it rejects a
		// path, kept here so the wording stays tied to the resolver it
		// describes rather than drifting into a generic message.
		deniedContains string
	}{
		"read":  {resolveReadPath, true, false, "outside the allowed roots"},
		"edit":  {resolveEditPath, false, false, "outside the writable session directory"},
		"write": {resolveWritePath, false, true, "outside the writable session directory"},
	}

	for name, r := range resolvers {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			skillsDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(skillsDir, "script.py"), []byte("print('ok')\n"), 0644); err != nil {
				t.Fatalf("failed to write skill file: %v", err)
			}
			sessionID := fmt.Sprintf("%s-%s", t.Name(), name)

			// Seed a real file in the session dir so the "existing leaf"
			// cases have something to resolve to.
			sessionPath, err := GetSessionPath(sessionID, skillsDir)
			if err != nil {
				t.Fatalf("GetSessionPath() error = %v", err)
			}
			if err := os.WriteFile(filepath.Join(sessionPath, "notes.txt"), []byte("hi\n"), 0644); err != nil {
				t.Fatalf("failed to seed session file: %v", err)
			}

			t.Run("allows a path inside the session directory", func(t *testing.T) {
				if _, err := r.fn(sessionID, skillsDir, "notes.txt"); err != nil {
					t.Errorf("expected session-dir path to be allowed, got %v", err)
				}
			})

			t.Run("rejects a path outside every root", func(t *testing.T) {
				_, err := r.fn(sessionID, skillsDir, "/etc/passwd")
				if err == nil {
					t.Fatal("expected a path outside the allowed roots to be rejected")
				}
				if !strings.Contains(err.Error(), r.deniedContains) {
					t.Errorf("expected the error to name %q, got %v", r.deniedContains, err)
				}
			})

			t.Run("skills directory", func(t *testing.T) {
				resolved, err := r.fn(sessionID, skillsDir, "skills/script.py")
				if r.allowsSkillsDir {
					if err != nil {
						t.Fatalf("expected the skills dir to be readable, got %v", err)
					}
					// The skills dir is reached through a symlink in the
					// session dir, so a resolver that returned the unresolved
					// path would still look "allowed" -- assert it resolved
					// through to the real file.
					want, evalErr := filepath.EvalSymlinks(filepath.Join(skillsDir, "script.py"))
					if evalErr != nil {
						t.Fatalf("EvalSymlinks() error = %v", evalErr)
					}
					if resolved != want {
						t.Errorf("resolved to %q, want %q", resolved, want)
					}
					return
				}
				if err == nil {
					t.Fatal("expected the read-only skills dir to be rejected for mutation")
				}
				if !strings.Contains(err.Error(), r.deniedContains) {
					t.Errorf("expected the error to name %q, got %v", r.deniedContains, err)
				}
			})

			t.Run("not-yet-existing leaf", func(t *testing.T) {
				_, err := r.fn(sessionID, skillsDir, "brand-new-file.txt")
				if r.allowsNewLeaf && err != nil {
					t.Errorf("expected a new file in the session dir to be allowed, got %v", err)
				}
				if !r.allowsNewLeaf && err == nil {
					t.Error("expected a nonexistent path to be rejected")
				}
			})

			t.Run("rejects traversal escaping the session directory", func(t *testing.T) {
				if _, err := r.fn(sessionID, skillsDir, "../../../etc/passwd"); err == nil {
					t.Error("expected ../ traversal out of the session dir to be rejected")
				}
			})
		})
	}
}

func TestNewSkillExecutionTools_ReturnsExpectedToolSet(t *testing.T) {
	skillsDir := t.TempDir()
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

	tools, err := NewSkillExecutionTools(skillsDir)
	if err != nil {
		t.Fatalf("NewSkillExecutionTools() error = %v", err)
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

func TestNewSkillExecutionTools_OmitsListFilesAndGrepFileByDefault(t *testing.T) {
	skillsDir := t.TempDir()
	t.Setenv("KAGENT_ENABLE_FILE_SEARCH_TOOLS", "")

	tools, err := NewSkillExecutionTools(skillsDir)
	if err != nil {
		t.Fatalf("NewSkillExecutionTools() error = %v, want nil (list_files/grep_file should be omitted, not fatal)", err)
	}

	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.Name()] = true
	}

	for _, name := range []string{"read_file", "write_file", "edit_file", "bash"} {
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

func TestNewSkillExecutionTools_BashDescriptionMentionsFileSearchToolsOnlyWhenEnabled(t *testing.T) {
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

	t.Run("disabled by default", func(t *testing.T) {
		t.Setenv("KAGENT_ENABLE_FILE_SEARCH_TOOLS", "")
		tools, err := NewSkillExecutionTools(skillsDir)
		if err != nil {
			t.Fatalf("NewSkillExecutionTools() error = %v", err)
		}
		desc := findBash(t, tools).Description()
		if strings.Contains(desc, "list_files") || strings.Contains(desc, "grep_file") {
			t.Errorf("bash description should not mention list_files/grep_file when disabled, got %q", desc)
		}
	})

	t.Run("mentioned when enabled", func(t *testing.T) {
		t.Setenv("KAGENT_ENABLE_FILE_SEARCH_TOOLS", "true")
		tools, err := NewSkillExecutionTools(skillsDir)
		if err != nil {
			t.Fatalf("NewSkillExecutionTools() error = %v", err)
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
// closures in NewSkillExecutionTools correctly wire path resolution and
// argument parsing end-to-end.
func TestListFilesAndGrepFileTools_RunThroughADK(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	skillsDir := t.TempDir()
	t.Setenv("KAGENT_ENABLE_FILE_SEARCH_TOOLS", "true")

	tools, err := NewSkillExecutionTools(skillsDir)
	if err != nil {
		t.Fatalf("NewSkillExecutionTools() error = %v", err)
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
	sessionPath, err := GetSessionPath(sessionID, skillsDir)
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

	// Exercised here, through the registered tool rather than against
	// ReadFileContent directly, because the failure this guards was invisible
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
