package indexer

import (
	"path/filepath"
	"strings"
)

// chunkMarkdown splits markdown content by heading boundaries.
func chunkMarkdown(filePath string, content []byte) []Chunk {
	text := string(content)
	lines := strings.Split(text, "\n")

	type section struct {
		heading string
		start   int // 1-indexed line number
		lines   []string
	}

	var sections []section
	current := section{start: 1}
	hasHeadings := false

	for i, line := range lines {
		if isMarkdownHeading(line) {
			hasHeadings = true
			// Flush the current section
			if len(current.lines) > 0 {
				sections = append(sections, current)
			}
			current = section{
				heading: extractHeadingText(line),
				start:   i + 1,
				lines:   []string{line},
			}
		} else {
			current.lines = append(current.lines, line)
		}
	}
	// Flush final section
	if len(current.lines) > 0 {
		sections = append(sections, current)
	}

	if !hasHeadings {
		return chunkWholeFile(filePath, content, "document")
	}

	var chunks []Chunk
	for _, s := range sections {
		body := strings.Join(s.lines, "\n")
		if strings.TrimSpace(body) == "" {
			continue
		}
		name := s.heading
		if name == "" {
			name = filepath.Base(filePath)
		}
		chunks = append(chunks, Chunk{
			FilePath:  filePath,
			LineStart: s.start,
			LineEnd:   s.start + len(s.lines) - 1,
			ChunkType: "heading",
			ChunkName: name,
			Content:   body,
		})
	}

	if len(chunks) == 0 {
		return chunkWholeFile(filePath, content, "document")
	}

	return chunks
}

func isMarkdownHeading(line string) bool {
	return strings.HasPrefix(line, "# ") ||
		strings.HasPrefix(line, "## ") ||
		strings.HasPrefix(line, "### ") ||
		strings.HasPrefix(line, "#### ")
}

func extractHeadingText(line string) string {
	return strings.TrimSpace(strings.TrimLeft(line, "# "))
}
