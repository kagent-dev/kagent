// Package config loads explicit CLI and controller configuration.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"gopkg.in/yaml.v3"
)

// Load decodes one YAML or JSON document. Defaults and validation run after
// explicit command-line overrides have been applied.
func Load(path string, dst proto.Message) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config %s: %w", path, err)
	}
	if err := Decode(data, dst); err != nil {
		return fmt.Errorf("failed to decode config %s: %w", path, err)
	}
	return nil
}

// Decode preserves scalar types and rejects duplicates before a map can discard
// them. Aliases and merge keys are deliberately outside the config format.
func Decode(data []byte, dst proto.Message) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		return fmt.Errorf("invalid configuration document: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("configuration must contain exactly one document")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("configuration must be an object")
	}
	value, err := jsonValue(doc.Content[0], "config")
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to encode configuration: %w", err)
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(encoded, dst); err != nil {
		return fmt.Errorf("invalid configuration fields: %w", err)
	}
	return nil
}

func jsonValue(node *yaml.Node, path string) (any, error) {
	switch node.Kind {
	case yaml.MappingNode:
		values := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" {
				return nil, fmt.Errorf("%s: keys must be strings", path)
			}
			if _, exists := values[key.Value]; exists {
				return nil, fmt.Errorf("%s.%s: duplicate field", path, key.Value)
			}
			value, err := jsonValue(node.Content[i+1], path+"."+key.Value)
			if err != nil {
				return nil, err
			}
			values[key.Value] = value
		}
		return values, nil
	case yaml.SequenceNode:
		values := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := jsonValue(child, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		return values, nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str":
			return node.Value, nil
		case "!!bool":
			value, err := strconv.ParseBool(strings.ToLower(node.Value))
			if err == nil {
				return value, nil
			}
		case "!!int", "!!float":
			if json.Valid([]byte(node.Value)) {
				return json.Number(node.Value), nil
			}
		}
	}
	return nil, fmt.Errorf("%s: unsupported value (null, aliases, and non-JSON scalars are not allowed)", path)
}

// Validate evaluates the source .proto rules without accessing external state.
func Validate(cfg proto.Message) error {
	if err := protovalidate.Validate(cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	return nil
}

// defaults fills absent fields, recursively. Unlike proto.Merge it never appends
// lists, and optional scalar presence preserves explicit false, zero, or empty.
func defaults(dst, src proto.Message) {
	fillDefaults(dst.ProtoReflect(), src.ProtoReflect())
}

func fillDefaults(dst, src protoreflect.Message) {
	src.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if !dst.Has(field) {
			if field.Kind() == protoreflect.MessageKind && !field.IsMap() && !field.IsList() {
				dst.Set(field, protoreflect.ValueOfMessage(proto.Clone(value.Message().Interface()).ProtoReflect()))
			} else {
				dst.Set(field, value)
			}
		} else if field.Kind() == protoreflect.MessageKind && !field.IsMap() && !field.IsList() {
			// Well-known values (especially Duration{seconds:0}) are atomic.
			if !strings.HasPrefix(string(field.Message().FullName()), "google.protobuf.") {
				fillDefaults(dst.Get(field).Message(), value.Message())
			}
		}
		return true
	})
}

// FilePath resolves paths in a document relative to that document.
func FilePath(configPath, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(filepath.Dir(configPath), value)
}

// ReadSecret reads a referenced secret without including its contents in errors.
func ReadSecret(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("secret file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file %s: %w", path, err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("secret file %s is empty", path)
	}
	return value, nil
}
