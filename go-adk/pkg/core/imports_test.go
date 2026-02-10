package core

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoGoogleADKImports ensures that the core package never imports packages
// from google.golang.org/adk. This test enforces architectural boundaries
// to keep the core package independent of the Google ADK implementation.
func TestNoGoogleADKImports(t *testing.T) {
	// Get the directory of the core package
	coreDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}

	// Find all Go files in the core package
	goFiles, err := filepath.Glob(filepath.Join(coreDir, "*.go"))
	if err != nil {
		t.Fatalf("failed to glob Go files: %v", err)
	}

	if len(goFiles) == 0 {
		t.Fatal("no Go files found in core package")
	}

	fset := token.NewFileSet()
	forbiddenPrefix := "google.golang.org/adk"

	for _, file := range goFiles {
		// Parse the Go file to extract imports
		f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("failed to parse file %s: %v", file, err)
			continue
		}

		for _, imp := range f.Imports {
			// Remove quotes from import path
			importPath := strings.Trim(imp.Path.Value, `"`)

			if strings.HasPrefix(importPath, forbiddenPrefix) {
				t.Errorf("file %s imports forbidden package %q (imports from %s are not allowed in core package)",
					filepath.Base(file), importPath, forbiddenPrefix)
			}
		}
	}
}
