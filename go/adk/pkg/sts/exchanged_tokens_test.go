package sts

import (
	"testing"

	"log/slog"

	kagentmodels "github.com/kagent-dev/kagent/go/adk/pkg/models"
)

// Every consumer takes the provider at construction and screens on the interface
// being nil, so a nil plugin assigned straight into the interface would read as
// "STS is configured" and send the caller's own credential wherever the
// exchanged one belongs. ExchangedTokens is the one place that conversion
// happens; if it stops collapsing nil, that breakage is silent at runtime.
func TestExchangedTokens(t *testing.T) {
	t.Parallel()

	t.Run("a nil plugin collapses to a nil interface", func(t *testing.T) {
		t.Parallel()
		if got := ExchangedTokens(nil); got != nil {
			t.Fatalf("ExchangedTokens(nil) = %#v, want a nil models.ExchangedTokenProvider", got)
		}
	})

	t.Run("a plugin is returned as the provider", func(t *testing.T) {
		t.Parallel()
		plugin := NewTokenPropagationPlugin(nil, slog.New(slog.DiscardHandler), nil, nil)

		got := ExchangedTokens(plugin)
		if got == nil {
			t.Fatal("ExchangedTokens(plugin) = nil, want the plugin")
		}
		if got != kagentmodels.ExchangedTokenProvider(plugin) {
			t.Fatalf("ExchangedTokens(plugin) = %#v, want the plugin itself", got)
		}
	})
}
