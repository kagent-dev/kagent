package a2a

import (
	"context"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// MCPRequestMetadataInterceptor validates request metadata before execution.
func MCPRequestMetadataInterceptor() a2asrv.CallInterceptor {
	return &mcpRequestMetadataInterceptor{}
}

type mcpRequestMetadataInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

func (*mcpRequestMetadataInterceptor) Before(ctx context.Context, _ *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	if send, ok := request.Payload.(*a2atype.SendMessageRequest); ok && send != nil {
		if err := apia2a.ValidateMCPRequestMetadata(send.Metadata); err != nil {
			return ctx, nil, fmt.Errorf("%w: %w", a2atype.ErrInvalidParams, err)
		}
	}
	return ctx, nil, nil
}
