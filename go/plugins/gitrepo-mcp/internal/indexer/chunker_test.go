package indexer

import (
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.js", "javascript"},
		{"index.jsx", "javascript"},
		{"app.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"Main.java", "java"},
		{"lib.rs", "rust"},
		{"README.md", "markdown"},
		{"doc.mdx", "markdown"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"pyproject.toml", "toml"},
		{"build.groovy", "groovy"},
		{"build.gradle", "groovy"},
		{"unknown.xyz", ""},
		{"Makefile", ""},
		{"Dockerfile", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := DetectLanguage(tt.path)
			if got != tt.want {
				t.Errorf("DetectLanguage(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestChunkMarkdownFile(t *testing.T) {
	source := `# Project Title

Introduction text here.

## Getting Started

Setup instructions.

## API Reference

API documentation.

### Endpoints

Endpoint details.
`
	chunks, err := ChunkFile("README.md", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	for _, c := range chunks {
		if c.ChunkType != "heading" {
			t.Errorf("chunk %q has type %q, want heading", c.ChunkName, c.ChunkType)
		}
	}

	found := findChunkByName(chunks, "Project Title")
	if found == nil {
		t.Fatal("expected chunk for Project Title")
	}
}

func TestChunkMarkdownNoHeadings(t *testing.T) {
	source := `Just some text without any headings.

Multiple paragraphs.
`
	chunks, err := ChunkFile("notes.md", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 whole-file chunk, got %d", len(chunks))
	}
	if chunks[0].ChunkType != "document" {
		t.Errorf("chunk type = %q, want document", chunks[0].ChunkType)
	}
}

func TestChunkYAMLFile(t *testing.T) {
	source := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`
	chunks, err := ChunkFile("config.yaml", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].ChunkType != "document" {
		t.Errorf("chunk type = %q, want document", chunks[0].ChunkType)
	}
	if chunks[0].ChunkName != "config.yaml" {
		t.Errorf("chunk name = %q, want config.yaml", chunks[0].ChunkName)
	}
	if chunks[0].LineStart != 1 {
		t.Errorf("LineStart = %d, want 1", chunks[0].LineStart)
	}
}

func TestChunkTOMLFile(t *testing.T) {
	source := `[package]
name = "my-project"
version = "0.1.0"
`
	chunks, err := ChunkFile("pyproject.toml", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].ChunkType != "document" {
		t.Errorf("chunk type = %q, want document", chunks[0].ChunkType)
	}
}

func TestChunkGroovyFile(t *testing.T) {
	source := `pipeline {
    agent any
    stages {
        stage('Build') {
            steps { sh 'make' }
        }
    }
}
`
	chunks, err := ChunkFile("Jenkinsfile.groovy", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 whole-file chunk for groovy, got %d", len(chunks))
	}
	if chunks[0].ChunkType != "document" {
		t.Errorf("chunk type = %q, want document", chunks[0].ChunkType)
	}
}

func TestChunkUnknownExtension(t *testing.T) {
	source := `some random binary-ish content`

	chunks, err := ChunkFile("data.bin", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].ChunkType != "document" {
		t.Errorf("chunk type = %q, want document", chunks[0].ChunkType)
	}
}

func TestChunkEmptyFile(t *testing.T) {
	chunks, err := ChunkFile("empty.go", []byte(""))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty file, got %d", len(chunks))
	}
}

func TestChunkWhitespaceOnly(t *testing.T) {
	chunks, err := ChunkFile("blank.go", []byte("   \n  \n  "))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for whitespace-only file, got %d", len(chunks))
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single line no newline", "hello", 1},
		{"single line with newline", "hello\n", 1},
		{"two lines", "hello\nworld", 2},
		{"two lines with trailing", "hello\nworld\n", 2},
		{"three lines", "a\nb\nc\n", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countLines([]byte(tt.input))
			if got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestChunkCodeFallback verifies that code files produce chunks even without CGo.
// With CGo: tree-sitter extracts structural chunks.
// Without CGo: falls back to whole-file document chunk.
func TestChunkCodeFallback(t *testing.T) {
	source := `package main

func hello() {}
`
	chunks, err := ChunkFile("main.go", []byte(source))
	if err != nil {
		t.Fatalf("ChunkFile failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk for a Go file")
	}
}

// helpers

func findChunkByName(chunks []Chunk, name string) *Chunk {
	for i := range chunks {
		if chunks[i].ChunkName == name {
			return &chunks[i]
		}
	}
	return nil
}
