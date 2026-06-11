package runner

import (
	"fmt"
	"sync"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	adkplugin "google.golang.org/adk/plugin"
)

// newMaxLLMCallsPlugin returns a plugin that enforces a limit on the number of
// LLM calls within a single invocation. The Go ADK has no native equivalent of
// the Python RunConfig.max_llm_calls, so this is implemented as a
// BeforeModelCallback that counts model calls per invocation ID and aborts the
// run when the limit is exceeded.
func newMaxLLMCallsPlugin(limit int) (*adkplugin.Plugin, error) {
	var mu sync.Mutex
	counts := make(map[string]int)

	return adkplugin.New(adkplugin.Config{
		Name: "kagent_max_llm_calls",
		BeforeModelCallback: func(ctx agent.CallbackContext, llmRequest *model.LLMRequest) (*model.LLMResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			id := ctx.InvocationID()
			counts[id]++
			if counts[id] > limit {
				return nil, fmt.Errorf(
					"agent stopped: exceeded the configured limit of %d model calls in a single run (reliability.maxLLMCalls)",
					limit,
				)
			}
			return nil, nil
		},
		AfterRunCallback: func(ictx agent.InvocationContext) {
			mu.Lock()
			defer mu.Unlock()
			delete(counts, ictx.InvocationID())
		},
	})
}
