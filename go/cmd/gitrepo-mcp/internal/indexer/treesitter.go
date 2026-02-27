//go:build cgo

package indexer

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// nodeRule defines which tree-sitter node types to extract as chunks.
type nodeRule struct {
	NodeType  string // tree-sitter AST node type
	ChunkType string // our classification (function, method, class, etc.)
	NameField string // field name to extract the identifier, or "" for special handling
}

// langConfig holds tree-sitter language and extraction rules.
type langConfig struct {
	Language *sitter.Language
	Rules    []nodeRule
}

var langConfigs = map[string]langConfig{
	"go": {
		Language: golang.GetLanguage(),
		Rules: []nodeRule{
			{"function_declaration", "function", "name"},
			{"method_declaration", "method", "name"},
			{"type_declaration", "type", ""},
		},
	},
	"python": {
		Language: python.GetLanguage(),
		Rules: []nodeRule{
			{"function_definition", "function", "name"},
			{"class_definition", "class", "name"},
		},
	},
	"javascript": {
		Language: javascript.GetLanguage(),
		Rules: []nodeRule{
			{"function_declaration", "function", "name"},
			{"class_declaration", "class", "name"},
		},
	},
	"typescript": {
		Language: typescript.GetLanguage(),
		Rules: []nodeRule{
			{"function_declaration", "function", "name"},
			{"class_declaration", "class", "name"},
		},
	},
	"java": {
		Language: java.GetLanguage(),
		Rules: []nodeRule{
			{"method_declaration", "method", "name"},
			{"class_declaration", "class", "name"},
			{"interface_declaration", "interface", "name"},
		},
	},
	"rust": {
		Language: rust.GetLanguage(),
		Rules: []nodeRule{
			{"function_item", "function", "name"},
			{"impl_item", "impl", "type"},
			{"struct_item", "struct", "name"},
		},
	},
}

// chunkWithTreeSitter parses source code and extracts structural chunks.
func chunkWithTreeSitter(filePath string, content []byte, lang string) ([]Chunk, error) {
	cfg, ok := langConfigs[lang]
	if !ok {
		return nil, fmt.Errorf("no tree-sitter config for language: %s", lang)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(cfg.Language)

	tree, err := parser.ParseCtx(context.Background(), nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed for %s: %w", filePath, err)
	}
	defer tree.Close()

	root := tree.RootNode()
	return findChunks(root, content, cfg.Rules, filePath), nil
}

// findChunks recursively walks the AST and collects chunks matching any rule.
func findChunks(node *sitter.Node, source []byte, rules []nodeRule, filePath string) []Chunk {
	var chunks []Chunk

	for _, rule := range rules {
		if node.Type() == rule.NodeType {
			name := extractNodeName(node, source, rule)
			endRow := int(node.EndPoint().Row)
			if node.EndPoint().Column == 0 && endRow > 0 {
				endRow--
			}
			chunk := Chunk{
				FilePath:  filePath,
				LineStart: int(node.StartPoint().Row) + 1,
				LineEnd:   endRow + 1,
				ChunkType: rule.ChunkType,
				ChunkName: name,
				Content:   node.Content(source),
			}
			chunks = append(chunks, chunk)
		}
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		chunks = append(chunks, findChunks(child, source, rules, filePath)...)
	}

	return chunks
}

// extractNodeName attempts to extract the identifier name from a matched node.
func extractNodeName(node *sitter.Node, source []byte, rule nodeRule) string {
	// Special case: Go type_declaration has nested type_spec with the name
	if node.Type() == "type_declaration" {
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child.Type() == "type_spec" {
				if nameNode := child.ChildByFieldName("name"); nameNode != nil {
					return nameNode.Content(source)
				}
			}
		}
		return ""
	}

	if rule.NameField != "" {
		if nameNode := node.ChildByFieldName(rule.NameField); nameNode != nil {
			return nameNode.Content(source)
		}
	}

	return ""
}
