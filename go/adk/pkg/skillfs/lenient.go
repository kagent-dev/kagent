// Package skillfs adapts a skills directory for the Go ADK's skill loader.
//
// The ADK's skill.Parse decodes a SKILL.md frontmatter with KnownFields and
// knows the agentskills.io fields only: name, description, license,
// compatibility, metadata and allowed-tools. Skills written for other harnesses
// carry more — Claude Code's user-invocable, argument-hint and
// disable-model-invocation, a top-level version — and one such field failed the
// whole agent ("field user-invocable not found in type skill.Frontmatter"),
// while the same skill loaded on the Python runtime (whose Frontmatter model
// allows extra fields) and in Claude Code. Lenient serves every SKILL.md with
// its frontmatter reduced to the fields the ADK knows, so the ADK's parser,
// validation and resource rules stay the single authority on what a skill is.
package skillfs

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// skillFile is the skill definition the ADK reads from every skill directory.
const skillFile = "SKILL.md"

// knownFields are the frontmatter fields skill.Frontmatter decodes:
// https://agentskills.io/specification#frontmatter.
var knownFields = map[string]bool{
	"name": true, "description": true, "license": true, "compatibility": true, "metadata": true, "allowed-tools": true,
}

// Lenient returns a filesystem that serves fsys unchanged, except that the
// SKILL.md at the root of every skill directory carries only the frontmatter
// fields the ADK knows. A SKILL.md the filter cannot read as frontmatter (no
// opening or closing separator, YAML that does not parse, a document that is
// not a mapping) is served unchanged, so the ADK reports it the way it always
// did.
func Lenient(fsys fs.FS) fs.FS {
	return &lenientFS{fsys: fsys}
}

type lenientFS struct {
	fsys fs.FS
}

// Open serves a skill's SKILL.md with its frontmatter filtered and every other
// path as fsys does. Directory listings come from fsys, so a listed SKILL.md
// entry reports the source file's size; the ADK reads sizes from neither.
func (l *lenientFS) Open(name string) (fs.File, error) {
	file, err := l.fsys.Open(name)
	if err != nil || !isSkillFile(name) {
		return file, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() {
		return file, nil
	}
	content, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", name, err)
	}
	filtered, err := DropUnknownFields(content)
	if err != nil {
		// Not frontmatter the filter understands: the ADK's own parser
		// reports it.
		filtered = content
	}
	return &memFile{Reader: bytes.NewReader(filtered), info: fileInfo{FileInfo: info, size: int64(len(filtered))}}, nil
}

// isSkillFile reports whether name is the SKILL.md at the root of a skill
// directory (an immediate subdirectory of the filesystem root).
func isSkillFile(name string) bool {
	dir, base := path.Split(name)
	dir = path.Clean(dir)
	return base == skillFile && dir != "." && dir != "/" && !strings.Contains(dir, "/")
}

var (
	separator    = []byte("---\n")
	separatorWin = []byte("---\r\n")

	errNotFrontmatter = errors.New("not a SKILL.md frontmatter")
)

func isSeparator(line []byte) bool {
	return bytes.Equal(line, separator) || bytes.Equal(line, separatorWin)
}

// DropUnknownFields returns content with every top-level frontmatter field the
// ADK does not know removed. Content whose frontmatter has no unknown field is
// returned as is. The frontmatter must be a YAML mapping between two separator
// lines, the way skill.Parse reads it; anything else is an error and the
// caller serves the content unchanged.
func DropUnknownFields(content []byte) ([]byte, error) {
	reader := bufio.NewReader(bytes.NewReader(content))
	line, err := reader.ReadBytes('\n')
	if err != nil || !isSeparator(line) {
		return nil, errNotFrontmatter
	}
	opening := line
	var frontmatter bytes.Buffer
	for {
		line, err = reader.ReadBytes('\n')
		if err != nil {
			return nil, errNotFrontmatter
		}
		if isSeparator(line) {
			break
		}
		frontmatter.Write(line)
	}
	closing := line

	var document yaml.Node
	if err := yaml.Unmarshal(frontmatter.Bytes(), &document); err != nil {
		return nil, fmt.Errorf("%w: %v", errNotFrontmatter, err)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errNotFrontmatter
	}
	mapping := document.Content[0]
	kept := mapping.Content[:0:0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if knownFields[mapping.Content[i].Value] {
			kept = append(kept, mapping.Content[i], mapping.Content[i+1])
		}
	}
	if len(kept) == len(mapping.Content) {
		return content, nil
	}
	mapping.Content = kept

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var out bytes.Buffer
	out.Write(opening)
	if len(kept) > 0 {
		encoder := yaml.NewEncoder(&out)
		encoder.SetIndent(2)
		if err := encoder.Encode(&document); err != nil {
			return nil, fmt.Errorf("encode frontmatter: %w", err)
		}
		if err := encoder.Close(); err != nil {
			return nil, fmt.Errorf("encode frontmatter: %w", err)
		}
	}
	out.Write(closing)
	out.Write(body)
	return out.Bytes(), nil
}

// memFile is a read-only fs.File over filtered content.
type memFile struct {
	*bytes.Reader
	info fileInfo
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Close() error               { return nil }

// fileInfo reports the source file's metadata with the filtered size.
type fileInfo struct {
	fs.FileInfo
	size int64
}

func (i fileInfo) Size() int64 { return i.size }
