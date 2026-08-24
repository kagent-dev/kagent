package output

import (
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
