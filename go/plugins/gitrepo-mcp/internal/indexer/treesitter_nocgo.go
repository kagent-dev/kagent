//go:build !cgo

package indexer

import "fmt"

// chunkWithTreeSitter is a no-op fallback when CGo is not available.
// All tree-sitter-supported languages fall back to whole-file chunking.
func chunkWithTreeSitter(filePath string, content []byte, lang string) ([]Chunk, error) {
	return nil, fmt.Errorf("tree-sitter chunking requires CGo (CGO_ENABLED=1)")
}
