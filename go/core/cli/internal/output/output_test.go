package output

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	for _, value := range []string{"table", "json"} {
		format, err := Parse(value)
		require.NoError(t, err)
		assert.Equal(t, Format(value), format)
	}

	_, err := Parse("yaml")
	require.Error(t, err)
}

func TestWriteJSON(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, WriteJSON(&output, struct {
		Name string `json:"name"`
	}{Name: "template"}))
	assert.True(t, json.Valid(output.Bytes()))
	assert.JSONEq(t, `{"name":"template"}`, output.String())
}
