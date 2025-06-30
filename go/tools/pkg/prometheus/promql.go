package prometheus

import (
	"context"
	_ "embed"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

//go:embed promql_prompt.md
var promqlPrompt string

func handlePromql(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	queryDescription := mcp.ParseString(request, "query_description", "")
	if queryDescription == "" {
		return mcp.NewToolResultError("query_description is required"), nil
	}

	llm, err := openai.New()
	if err != nil {
		return mcp.NewToolResultError("failed to create LLM client: " + err.Error()), nil
	}

	contents := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: promqlPrompt},
			},
		},

		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: queryDescription},
			},
		},
	}

	resp, err := llm.GenerateContent(ctx, contents, llms.WithModel("gpt-4o-mini"))
	if err != nil {
		return mcp.NewToolResultError("failed to generate content: " + err.Error()), nil
	}

	choices := resp.Choices
	if len(choices) < 1 {
		return mcp.NewToolResultError("empty response from model"), nil
	}
	c1 := choices[0]
	return mcp.NewToolResultText(c1.Content), nil
}
