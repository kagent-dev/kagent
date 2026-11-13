package prompt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfirm(t *testing.T) {
	for answer, want := range map[string]bool{"y\n": true, "YES\n": true, " yes ": true, "\n": false, "n\n": false, "": false, "yep\n": false} {
		var out bytes.Buffer
		got, err := Confirm(strings.NewReader(answer), &out, "Delete them?")
		require.NoError(t, err)
		require.Equal(t, want, got, "answer %q", answer)
		require.Equal(t, "Delete them? [y/N]: ", out.String())
	}
}
