package mcp

import (
	"context"

	"buf.build/go/protovalidate"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
)

type listAgentsInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"Agent namespace; omit to list all visible namespaces"`
	PageSize  int32  `json:"page_size,omitempty" jsonschema:"Maximum number of Agents to return (1-100); omit or use 0 for all"`
	PageToken string `json:"page_token,omitempty" jsonschema:"Token returned by a previous call; keep the same namespace filter"`
}

type listAgentsOutput struct {
	Agents        []agentReference `json:"agents"`
	NextPageToken string           `json:"next_page_token,omitempty"`
}

func (h *Handler) listAgents(ctx context.Context, _ *mcp.CallToolRequest, input listAgentsInput) (*mcp.CallToolResult, listAgentsOutput, error) {
	request := &apiv1.ListAgentsRequest{
		Namespace: input.Namespace,
		Page:      &apiv1.PageRequest{Limit: input.PageSize, PageToken: input.PageToken},
	}
	if err := protovalidate.Validate(request); err != nil {
		return toolError(err), listAgentsOutput{}, nil
	}
	items, err := h.agents.List(ctx, request.GetNamespace())
	if err != nil {
		return toolError(err), listAgentsOutput{}, nil
	}
	items, next, err := kubecrud.Page(items, int(request.GetPage().GetLimit()), request.GetPage().GetPageToken())
	if err != nil {
		return toolError(err), listAgentsOutput{}, nil
	}
	output := listAgentsOutput{Agents: make([]agentReference, 0, len(items)), NextPageToken: next}
	for _, item := range items {
		output.Agents = append(output.Agents, agentReference{Namespace: item.Namespace, Name: item.Name})
	}
	// Let the SDK include the references and continuation token in text content too.
	return nil, output, nil
}
