package a2a

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
)

func TestStructuredOutputJSON(t *testing.T) {
	part := a2atype.NewDataPart(map[string]any{"answer": float64(4)})
	part.MediaType = "application/json; charset=utf-8"
	part.SetMeta(OutputSchemaSHA256MetadataKey, "digest")

	text, err := StructuredOutputJSON(part)
	require.NoError(t, err)
	require.JSONEq(t, `{"answer":4}`, text)
}

func TestStructuredOutputJSONRejectsOrdinaryDataParts(t *testing.T) {
	part := a2atype.NewDataPart(map[string]any{"name": "tool", "response": "done"})
	part.MediaType = "application/json"

	text, err := StructuredOutputJSON(part)
	require.EqualError(t, err, "part is not structured output")
	require.Empty(t, text)
}
