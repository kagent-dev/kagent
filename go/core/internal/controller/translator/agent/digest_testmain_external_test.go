package agent_test

import (
	"os"
	"testing"

	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

func TestMain(m *testing.M) {
	translator.PythonADKImageDigest = "sha256:test-app"
	translator.GoADKImageDigest = "sha256:test-go-base"
	translator.GoADKFullImageDigest = "sha256:test-go-full"
	os.Exit(m.Run())
}
