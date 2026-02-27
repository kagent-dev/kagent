package search

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/kagent-dev/kagent/go/cmd/gitrepo-mcp/internal/storage"
)

// AstSearchResult represents a single ast-grep match.
type AstSearchResult struct {
	FilePath    string `json:"filePath"`
	LineStart   int    `json:"lineStart"`
	LineEnd     int    `json:"lineEnd"`
	Content     string `json:"content"`
	MatchedNode string `json:"matchedNode"`
	Language    string `json:"language"`
}

// astGrepMatch is the JSON structure returned by `ast-grep --json`.
type astGrepMatch struct {
	Text     string `json:"text"`
	Range    astRange `json:"range"`
	File     string `json:"file"`
	Language string `json:"language"`
}

type astRange struct {
	Start astPosition `json:"start"`
	End   astPosition `json:"end"`
}

type astPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// AstSearcher performs structural search using ast-grep.
type AstSearcher struct {
	repoStore *storage.RepoStore
}

// NewAstSearcher creates an AstSearcher.
func NewAstSearcher(repoStore *storage.RepoStore) *AstSearcher {
	return &AstSearcher{repoStore: repoStore}
}

// Search runs ast-grep structural search on a repository.
// pattern is the ast-grep pattern (e.g., "func $NAME($$$) error").
// lang is an optional language filter (e.g., "go", "python").
func (a *AstSearcher) Search(pattern string, repoName string, lang string) ([]AstSearchResult, error) {
	if pattern == "" {
		return nil, fmt.Errorf("pattern must not be empty")
	}

	repo, err := a.repoStore.Get(repoName)
	if err != nil {
		return nil, fmt.Errorf("repo %s not found: %w", repoName, err)
	}

	if repo.Status != storage.RepoStatusCloned && repo.Status != storage.RepoStatusIndexed {
		return nil, fmt.Errorf("repo %s is not ready (status: %s)", repoName, repo.Status)
	}

	return runAstGrep(pattern, repo.LocalPath, lang)
}

// runAstGrep shells out to the ast-grep binary and parses its JSON output.
func runAstGrep(pattern, repoPath, lang string) ([]AstSearchResult, error) {
	args := []string{"run", "--pattern", pattern, "--json=stream"}
	if lang != "" {
		args = append(args, "--lang", lang)
	}
	args = append(args, repoPath)

	cmd := exec.Command("ast-grep", args...)

	output, err := cmd.Output()
	if err != nil {
		if execErr, ok := err.(*exec.ExitError); ok {
			// ast-grep returns exit code 1 when no matches found
			if execErr.ExitCode() == 1 && len(output) == 0 {
				return nil, nil
			}
			// Other exit errors with stderr
			stderr := string(execErr.Stderr)
			if stderr != "" {
				return nil, fmt.Errorf("ast-grep failed: %s", strings.TrimSpace(stderr))
			}
		}
		if exec.ErrNotFound.Error() == err.Error() || isExecNotFound(err) {
			return nil, fmt.Errorf("ast-grep binary not found, install from https://ast-grep.github.io/")
		}
		return nil, fmt.Errorf("ast-grep failed: %w", err)
	}

	return parseAstGrepOutput(output, repoPath)
}

// isExecNotFound checks if the error indicates the binary was not found.
func isExecNotFound(err error) bool {
	return strings.Contains(err.Error(), "executable file not found")
}

// parseAstGrepOutput parses ast-grep --json=stream output (newline-delimited JSON).
func parseAstGrepOutput(data []byte, repoPath string) ([]AstSearchResult, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var results []AstSearchResult
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var match astGrepMatch
		if err := json.Unmarshal([]byte(line), &match); err != nil {
			return nil, fmt.Errorf("failed to parse ast-grep output: %w", err)
		}

		filePath := match.File
		if strings.HasPrefix(filePath, repoPath) {
			filePath = strings.TrimPrefix(filePath, repoPath)
			filePath = strings.TrimPrefix(filePath, "/")
		}

		results = append(results, AstSearchResult{
			FilePath:    filePath,
			LineStart:   match.Range.Start.Line + 1, // ast-grep uses 0-indexed lines
			LineEnd:     match.Range.End.Line + 1,
			Content:     match.Text,
			MatchedNode: match.Text,
			Language:    match.Language,
		})
	}

	return results, nil
}

// SupportedLanguages returns the list of languages supported by ast-grep.
func SupportedLanguages() []string {
	return []string{
		"c",
		"cpp",
		"css",
		"go",
		"html",
		"java",
		"javascript",
		"kotlin",
		"lua",
		"python",
		"rust",
		"swift",
		"typescript",
		"tsx",
	}
}
