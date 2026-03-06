package compaction

import "fmt"

// Config controls when and how session history compaction is triggered and executed.
// Compaction reduces token usage by summarizing old conversation history using the LLM,
// replacing the original events with a compressed summary while maintaining the full
// event history in storage for audit trail purposes.
//
// NOTE: This is a temporary implementation that mirrors the upstream API from
// https://github.com/google/adk-go/pull/300. Once upstream releases compaction
// support, this package should be deprecated in favor of google.golang.org/adk/compaction.
type Config struct {
	// Enabled controls whether compaction is active.
	// If false, no compaction will occur even if thresholds are exceeded.
	// Default: false
	Enabled bool

	// CompactionInterval specifies the number of unique invocations after which
	// compaction should be triggered. For example, if set to 5, compaction will
	// be triggered when 5 new invocations have occurred since the last compaction.
	//
	// An invocation is a single turn in the conversation (user message + agent response).
	// Compaction is based on invocation IDs, not individual events.
	//
	// Lower values = more frequent compaction (more API calls, lower token count)
	// Higher values = less frequent compaction (fewer API calls, higher token count)
	//
	// Sensible default: 5 (compact every 5 turns)
	CompactionInterval int

	// OverlapSize specifies the number of invocations to keep when compacting.
	// This ensures context continuity across compaction boundaries by maintaining
	// a sliding window of recent invocations.
	//
	// For example, if CompactionInterval=5 and OverlapSize=2:
	// - Events from invocations 1-5 are compacted into a summary
	// - Invocations 4-5 are kept as overlap
	// - The next compaction happens at invocation 10, including 4-10
	//
	// Sensible default: 2 (keep 2 invocations for context continuity)
	OverlapSize int

	// Model is the LLM model to use for generating compaction summaries.
	// If empty, will use the same model as the current invocation.
	// Example: "gemini-2.0-flash" or "gemini-1.5-pro"
	// Default: "" (uses current model)
	Model string

	// SystemPrompt is the system prompt for the LLM when generating summaries.
	// It should instruct the model on how to create concise, relevant summaries.
	// If empty, a default system prompt will be used.
	// Default: "" (uses built-in prompt)
	SystemPrompt string
}

// DefaultConfig returns a Config with sensible defaults matching the Python ADK.
// These defaults are tuned for typical conversational use cases.
func DefaultConfig() Config {
	return Config{
		Enabled:            false,
		CompactionInterval: 5,
		OverlapSize:        2,
		Model:              "",
		SystemPrompt:       "",
	}
}

// Validate checks that the configuration is valid.
// Returns an error if any required fields are invalid.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil // Disabled config is valid
	}

	if c.CompactionInterval <= 0 {
		return fmt.Errorf("compaction_interval must be positive when enabled, got %d", c.CompactionInterval)
	}

	if c.OverlapSize < 0 {
		return fmt.Errorf("overlap_size must be non-negative when enabled, got %d", c.OverlapSize)
	}

	if c.OverlapSize >= c.CompactionInterval {
		return fmt.Errorf("overlap_size (%d) must be less than compaction_interval (%d)", c.OverlapSize, c.CompactionInterval)
	}

	return nil
}
