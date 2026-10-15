package translator_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/config"
)

func TestRuntimeConfigCompilesA2ARetry(t *testing.T) {
	cfg := &configv1.ControllerConfig{}
	config.DefaultController(cfg)
	retry := &configv1.A2ARetryConfig{Enabled: true, MaxAttempts: new(uint32(4)), BaseDelay: durationpb.New(100 * time.Millisecond)}
	cfg.RuntimeDefaults.A2ARetry = retry
	require.NoError(t, config.Validate(cfg.RuntimeDefaults))
	runtime := translator.RuntimeConfig(cfg.RuntimeDefaults, "assistant", "team")
	require.True(t, proto.Equal(retry, runtime.GetA2ARetry()))
	require.NotSame(t, retry, runtime.GetA2ARetry())

	cfg.RuntimeDefaults.A2ARetry = &configv1.A2ARetryConfig{MaxAttempts: new(uint32(0))}
	require.Error(t, config.Validate(cfg.RuntimeDefaults))
}
