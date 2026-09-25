package translator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"istio.io/istio/pkg/kube/krt"
	"k8s.io/apimachinery/pkg/types"
)

const maxOutputSchemaBytes = 64 << 10

// portableOutputSchemaMetaSchema describes the JSON Schema subset that every
// supported kagent ADK integration can consume. It is deliberately itself a
// JSON Schema so keyword forms and recursive schema locations are validated by
// jsonschema-go rather than by a parallel handwritten schema implementation.
const portableOutputSchemaMetaSchema = `{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"$defs":{
		"schema":{
			"type":"object",
			"properties":{
				"$id":{"type":"string"},
				"$schema":{"type":"string"},
				"$defs":{"type":"object","additionalProperties":{"$ref":"#/$defs/schema"}},
				"$ref":{"type":"string","pattern":"^#/\\$defs/[^/]*$"},
				"additionalProperties":{"anyOf":[{"type":"boolean"},{"$ref":"#/$defs/schema"}]},
				"anyOf":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/schema"}},
				"const":{},
				"description":{"type":"string"},
				"enum":{"type":"array","minItems":1},
				"items":{"$ref":"#/$defs/schema"},
				"properties":{"type":"object","additionalProperties":{"$ref":"#/$defs/schema"}},
				"required":{"type":"array","items":{"type":"string","minLength":1},"uniqueItems":true},
				"title":{"type":"string"},
				"type":{"enum":["object","array","string","number","integer","boolean","null"]}
			},
			"additionalProperties":false
		}
	},
	"allOf":[{"$ref":"#/$defs/schema"}],
	"properties":{"type":{"const":"object"}},
	"required":["type"]
}`

var resolvePortableOutputSchemaMetaSchema = sync.OnceValues(func() (*jsonschema.Resolved, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(portableOutputSchemaMetaSchema), &schema); err != nil {
		return nil, fmt.Errorf("decode portable output meta-schema: %w", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve portable output meta-schema: %w", err)
	}
	return resolved, nil
})

// ResolvedOutputSchema is the canonical root output contract passed to a
// Harness compiler. Source is nil for an inline schema.
type ResolvedOutputSchema struct {
	Schema []byte
	SHA256 string
	Source *ResolvedConfigMapKeyReference
}

// ResolvedConfigMapKeyReference identifies the exact ConfigMap input used by a
// compiled revision.
type ResolvedConfigMapKeyReference struct {
	NamespacedName types.NamespacedName
	Key            string
}

func (c *Compiler) resolveOutputSchema(_ context.Context, template *TemplateConfiguration) (*ResolvedOutputSchema, error) {
	var raw []byte
	var source *ResolvedConfigMapKeyReference
	switch {
	case template.Spec.OutputSchema != nil:
		raw = template.Spec.OutputSchema.Raw
	case template.Spec.OutputSchemaFrom != nil:
		ref := template.Spec.OutputSchemaFrom
		key := types.NamespacedName{Namespace: template.Namespace, Name: ref.Name}
		configMap := krt.FetchOne(c.ctx, c.collections.ConfigMaps, krt.FilterObjectName(key))
		if configMap == nil {
			return nil, fmt.Errorf("resolve output schema ConfigMap %q: not found", ref.Name)
		}
		value, ok := (*configMap).Data[ref.Key]
		if !ok {
			return nil, fmt.Errorf("resolve output schema ConfigMap %q: key %q not found", ref.Name, ref.Key)
		}
		raw = []byte(value)
		source = &ResolvedConfigMapKeyReference{NamespacedName: key, Key: ref.Key}
	default:
		return nil, nil
	}

	canonical, err := validateOutputSchema(raw)
	if err != nil {
		return nil, NewValidationError("invalid output schema: %v", err)
	}
	digest := sha256.Sum256(canonical)
	return &ResolvedOutputSchema{Schema: canonical, SHA256: hex.EncodeToString(digest[:]), Source: source}, nil
}

// validateOutputSchema validates a document against kagent's portable JSON
// Schema profile, then asks jsonschema-go to parse and resolve the schema
// itself. Go ADK compatibility checks that depend on genai.Schema projection,
// such as reference expansion and cycle detection, run in the kagent Harness
// compiler.
func validateOutputSchema(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("schema is empty")
	}
	if len(raw) > maxOutputSchemaBytes {
		return nil, fmt.Errorf("schema exceeds %d bytes", maxOutputSchemaBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("schema must contain one JSON value")
		}
		return nil, fmt.Errorf("decode trailing JSON: %w", err)
	}

	profile, err := resolvePortableOutputSchemaMetaSchema()
	if err != nil {
		return nil, err
	}
	if err := profile.Validate(document); err != nil {
		return nil, fmt.Errorf("schema does not match the portable output profile: %w", err)
	}

	canonical, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("canonicalize schema: %w", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(canonical, &schema); err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}
	if _, err := schema.Resolve(nil); err != nil {
		return nil, fmt.Errorf("resolve schema: %w", err)
	}
	return canonical, nil
}
