package mcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPToolAliasesRemainDistinctAfterTruncation(t *testing.T) {
	// Given two names with a long common prefix, when they are aliased, assert
	// that the model receives distinct valid function names.
	set := &mcpAppToolset{toolPrefix: "mcp1_"}
	common := strings.Repeat("archive.", 9)
	first, second := set.toolName(common+"books"), set.toolName(common+"articles")
	require.Regexp(t, `^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`, first)
	require.Regexp(t, `^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`, second)
	require.NotEqual(t, first, second)
}
