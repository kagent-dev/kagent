package agent_test

import (
	"os"
	"testing"

	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

func TestMain(m *testing.M) {
	translator.AgentImageDigest = "sha256:test-go-base"
	os.Exit(m.Run())
}
