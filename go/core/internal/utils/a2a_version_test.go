package utils

import (
	"net/http/httptest"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNegotiateA2AWireVersion(t *testing.T) {
	t.Run("accepts v1 header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set(a2atype.SvcParamVersion, string(a2atype.Version))

		version, err := NegotiateA2AWireVersion(req)

		require.NoError(t, err)
		assert.Equal(t, A2AWireVersionV1, version)
	})

	t.Run("rejects missing header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)

		_, err := NegotiateA2AWireVersion(req)

		require.Error(t, err)
		assert.EqualError(t, err, `missing required A2A-Version header; expected "1.0"`)
	})

	t.Run("rejects legacy header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set(a2atype.SvcParamVersion, "0.3")

		_, err := NegotiateA2AWireVersion(req)

		require.Error(t, err)
		assert.EqualError(t, err, `unsupported A2A-Version "0.3"; expected "1.0"`)
	})
}
