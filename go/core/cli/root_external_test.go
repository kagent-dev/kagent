package cli_test

import (
	"testing"

	"github.com/kagent-dev/kagent/go/core/cli"
)

func TestRootIsExternallyImportable(t *testing.T) {
	root := cli.Root()
	if root == nil {
		t.Fatal("Root() returned nil")
	}
}
