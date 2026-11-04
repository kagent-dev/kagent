package a2a

import (
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func adkToolResponse(response map[string]any) (*apiv1.ToolResponse, error) {
	if message, ok := response["error"].(string); ok {
		return apia2a.NewToolResponse(message, true)
	}
	if response["status"] == "pending" {
		result, err := apia2a.NewToolResponse(response, false)
		if err != nil {
			return nil, err
		}
		result.Outcome = &apiv1.ToolResponse_Pending{Pending: result.GetOutput()}
		return result, nil
	}
	if output, ok := response["output"]; ok {
		failed, _ := response["isError"].(bool)
		return apia2a.NewMCPToolResponse(output, failed)
	}
	return apia2a.NewToolResponse(response, false)
}
