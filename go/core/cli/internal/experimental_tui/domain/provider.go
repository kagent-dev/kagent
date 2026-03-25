package domain

type ModelProvider interface {
	Name() string
	MaxContextTokens() int
	InputTokens() int
	OutputTokens() int
	TokenCost(input, output int) float64
	SupportsThinking() bool
}
