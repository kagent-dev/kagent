package bridge

import (
	"os"
	"testing"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/supervisor"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == supervisor.Mode {
		os.Exit(supervisor.Run(os.Args[2:]))
	}
	os.Exit(m.Run())
}
