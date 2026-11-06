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

	adkoutputschema "github.com/kagent-dev/kagent/go/adk/pkg/outputschema"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

const maxOutputSchemaBytes = 64 << 10

const portableOutputSchemaMetaSchemaTemplate = `{
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
				"format":{"enum":%s},
				"items":{"$ref":"#/$defs/schema"},
				"pattern":{"type":"string"},
				"properties":{"type":"object","additionalProperties":{"$ref":"#/$defs/schema"}},
				"required":{"type":"array","items":{"type":"string","minLength":1},"uniqueItems":true},
				"title":{"type":"string"},
				"type":{"anyOf":[{"$ref":"#/$defs/typeName"},{"type":"array","minItems":1,"uniqueItems":true,"items":{"$ref":"#/$defs/typeName"}}]}
			},
			"additionalProperties":false,
			"dependentSchemas":{
				"format":{"$ref":"#/$defs/stringSchema"},
				"pattern":{"$ref":"#/$defs/stringSchema"}
			}
		},
		"typeName":{"enum":["object","array","string","number","integer","boolean","null"]},
		"stringSchema":{"properties":{"type":{"anyOf":[{"const":"string"},{"type":"array","contains":{"const":"string"}}]}},"required":["type"]}
	},
	"allOf":[{"$ref":"#/$defs/schema"}],
	"properties":{"type":{"const":"object"}},
	"required":["type"]
}`

var resolvePortableOutputSchemaMetaSchema = sync.OnceValues(func() (*jsonschema.Resolved, error) {
	formats, err := json.Marshal(adkoutputschema.SupportedStringFormats())
	if err != nil {
		return nil, fmt.Errorf("encode supported output formats: %w", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(fmt.Appendf(nil, portableOutputSchemaMetaSchemaTemplate, formats), &schema); err != nil {
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
	Strict bool
}

// resolveConfigMapKey reads one key of a same-namespace ConfigMap. purpose names
// the input in errors. The fetch registers the ConfigMap as a dependency, so
// changes to it recompile the Agent.
func (c *Compiler) resolveConfigMapKey(namespace, purpose string, ref *v1alpha3.ConfigMapKeyReference) ([]byte, *ResolvedConfigMapKeyReference, error) {
	key := types.NamespacedName{Namespace: namespace, Name: ref.Name}
	configMap := krt.FetchOne(c.ctx, c.collections.ConfigMaps, krt.FilterObjectName(key))
	if configMap == nil {
		return nil, nil, fmt.Errorf("resolve %s ConfigMap %q: not found", purpose, ref.Name)
	}
	value, ok := (*configMap).Data[ref.Key]
	if !ok {
		return nil, nil, fmt.Errorf("resolve %s ConfigMap %q: key %q not found", purpose, ref.Name, ref.Key)
	}
	return []byte(value), &ResolvedConfigMapKeyReference{NamespacedName: key, Key: ref.Key}, nil
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
		var err error
		raw, source, err = c.resolveConfigMapKey(template.Namespace, "output schema", template.Spec.OutputSchemaFrom)
		if err != nil {
			return nil, err
		}
	default:
		return nil, nil
	}

	canonical, err := validateOutputSchema(raw)
	if err != nil {
		return nil, NewValidationError("invalid output schema: %v", err)
	}
	digest := sha256.Sum256(canonical)
	return &ResolvedOutputSchema{Schema: canonical, SHA256: hex.EncodeToString(digest[:]), Source: source, Strict: template.Spec.OutputSchemaStrict}, nil
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
	if err := adkoutputschema.CheckPatterns(&schema); err != nil {
		return nil, err
	}
	if _, err := schema.Resolve(nil); err != nil {
		return nil, fmt.Errorf("resolve schema: %w", err)
	}
	return canonical, nil
}
