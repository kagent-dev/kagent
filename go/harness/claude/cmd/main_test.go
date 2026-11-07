package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMissingBootstrapDescriptor(t *testing.T) {
	require.ErrorContains(t, run(t.Context(), true, func(string) string { return "" }, nil), "bootstrap descriptor")
}
