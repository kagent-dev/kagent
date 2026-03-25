package experimentaltui

import "github.com/kagent-dev/kagent/go/core/cli/internal/experimental_tui/domain"

type DemoProvider struct{}

var _ domain.ModelProvider = (*DemoProvider)(nil)

func NewDemoProvider() *DemoProvider {
	return &DemoProvider{}
}

func (a *DemoProvider) Name() string {
	return "Demo Model"
}

func (a *DemoProvider) MaxContextTokens() int {
	return 4096
}

func (a *DemoProvider) InputTokens() int {
	return 100
}

func (a *DemoProvider) OutputTokens() int {
	return 200
}

func (a *DemoProvider) TokenCost(input, output int) float64 {
	return float64(input+output) * 0.0001
}

func (a *DemoProvider) SupportsThinking() bool {
	return true
}
