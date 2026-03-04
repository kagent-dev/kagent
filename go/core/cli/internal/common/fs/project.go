// Package fs provides filesystem utilities for working with project directories.
package fs

import (
	"fmt"
	"os"
	"path/filepath"
)

// ProjectDir represents a project directory with validation and utility methods.
// It provides a consistent way to handle project paths across the CLI.
type ProjectDir struct {
	path    string
	verbose bool
}

// NewProjectDir creates a new ProjectDir, resolving relative paths to absolute paths.
// If path is empty, it uses the current working directory.
// If verbose is true, it logs the resolved directory path.
func NewProjectDir(path string, verbose bool) (*ProjectDir, error) {
	resolved, err := resolvePath(path)
	if err != nil {
		return nil, err
	}

	if verbose {
		fmt.Printf("Using project directory: %s\n", resolved)
	}

	return &ProjectDir{
		path:    resolved,
		verbose: verbose,
	}, nil
}

// resolvePath resolves a path to an absolute path.
// If the path is empty, it returns the current working directory.
// If the path is relative, it converts it to an absolute path.
func resolvePath(path string) (string, error) {
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
		return cwd, nil
	}

	if filepath.IsAbs(path) {
		return path, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return filepath.Join(cwd, path), nil
}

// Path returns the absolute path to the project directory.
func (p *ProjectDir) Path() string {
	return p.path
}

// Validate checks if the project directory exists.
// Returns an error if the directory doesn't exist or the path is empty.
func (p *ProjectDir) Validate() error {
	if p.path == "" {
		return fmt.Errorf("project directory is required")
	}

	if _, err := os.Stat(p.path); os.IsNotExist(err) {
		return fmt.Errorf("project directory does not exist: %s", p.path)
	}

	return nil
}

// Exists checks if the project directory exists.
// Returns true if the directory exists, false otherwise.
func (p *ProjectDir) Exists() bool {
	_, err := os.Stat(p.path)
	return err == nil
}

// Join joins path elements with the project directory.
// This is a convenience method for constructing paths within the project.
func (p *ProjectDir) Join(elem ...string) string {
	return filepath.Join(append([]string{p.path}, elem...)...)
}

// FileExists checks if a file exists in the project directory.
// The filename is relative to the project directory.
func (p *ProjectDir) FileExists(filename string) bool {
	_, err := os.Stat(p.Join(filename))
	return err == nil
}

// IsVerbose returns whether verbose mode is enabled for this project directory.
func (p *ProjectDir) IsVerbose() bool {
	return p.verbose
}

// FileExists checks if a file exists at the given path.
// This is a standalone utility function for checking file existence.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DirExists checks if a directory exists at the given path.
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// EnsureDir creates a directory if it doesn't exist.
// It creates all necessary parent directories as well.
func EnsureDir(path string) error {
	if DirExists(path) {
		return nil
	}
	return os.MkdirAll(path, 0755)
}
