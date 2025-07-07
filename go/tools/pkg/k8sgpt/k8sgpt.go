package k8sgpt

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/tools/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TODO:
// Extend support for additional k8sgpt options (e.g., --filter etc.) so users can customize the analysis.
// Add corresponding tests.

// k8sgptAnalyze handles the k8sgpt analyze command.
func handleK8sgptAnalyze(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	namespace := mcp.ParseString(request, "namespace", "")

	args := []string{"analyze"}

	if namespace != "" {
		args = append(args, "-n", namespace)
	}

	result, err := utils.RunCommandWithContext(ctx, "k8sgpt", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("k8sgpt analyze failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// Register K8sgpt tools
func RegisterK8sgptTools(s *server.MCPServer) {
	// Istio proxy status
	s.AddTool(mcp.NewTool("K8sgpt_Analyze",
		mcp.WithDescription("This command will find problems within your Kubernetes cluster"),
		mcp.WithString("namespace", mcp.Description("Namespace of the cluster to analyze")),
	), handleK8sgptAnalyze)
}
