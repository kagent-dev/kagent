package agent_test

import (
	"os"
	"testing"

	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

func TestMain(m *testing.M) {
	translator.DefaultAppImageDigest = "sha256:test-app"
	translator.DefaultGoImageDigest = "sha256:test-go-base"
	translator.DefaultGoFullImageDigest = "sha256:test-go-full"
	os.Exit(m.Run())
}
