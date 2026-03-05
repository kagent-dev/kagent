package indexer

import (
	"bytes"
	"path/filepath"
)

// Chunk represents a code chunk extracted from a source file.
type Chunk struct {
	FilePath  string // relative path within the repo
	LineStart int    // 1-indexed
	LineEnd   int    // 1-indexed, inclusive
	ChunkType string // "function", "method", "class", "type", "interface", "impl", "struct", "heading", "document"
	ChunkName string // identifier name (function name, class name, heading text, etc.)
	Content   string
}

// ChunkFile parses a source file and returns structural code chunks.
// Language is detected from the file extension.
func ChunkFile(filePath string, content []byte) ([]Chunk, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil
	}

	lang := DetectLanguage(filePath)

	switch lang {
	case "markdown":
		return chunkMarkdown(filePath, content), nil
	case "yaml", "toml", "groovy":
		return chunkWholeFile(filePath, content, "document"), nil
	case "":
		return chunkWholeFile(filePath, content, "document"), nil
	default:
		chunks, err := chunkWithTreeSitter(filePath, content, lang)
		if err != nil || len(chunks) == 0 {
			return chunkWholeFile(filePath, content, "document"), nil
		}
		return chunks, nil
	}
}

func chunkWholeFile(filePath string, content []byte, chunkType string) []Chunk {
	if len(content) == 0 {
		return nil
	}
	return []Chunk{{
		FilePath:  filePath,
		LineStart: 1,
		LineEnd:   countLines(content),
		ChunkType: chunkType,
		ChunkName: filepath.Base(filePath),
		Content:   string(content),
	}}
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	count := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		count++
	}
	return count
}
