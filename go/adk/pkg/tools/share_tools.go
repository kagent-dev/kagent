package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// shareClient holds the dependencies for share link tools, captured at construction time.
type shareClient struct {
	controllerClient *controllerclient.Client
	uiURL            string // KAGENT_UI_URL, used to build full share URLs
	appName          string
}

// parseAppName converts a Python-identifier app_name back to (namespace, name).
// Format: "namespace__NS__agent_name" with hyphens encoded as underscores.
func parseAppName(appName string) (namespace, name string) {
	parts := strings.SplitN(appName, "__NS__", 2)
	if len(parts) != 2 {
		return "", strings.ReplaceAll(appName, "_", "-")
	}
	return strings.ReplaceAll(parts[0], "_", "-"), strings.ReplaceAll(parts[1], "_", "-")
}

// shareURL returns the share URL for a session token.
// With uiURL set it returns a full absolute URL; otherwise a relative path.
func (c *shareClient) shareURL(token, sessionID string) string {
	ns, name := parseAppName(c.appName)
	path := fmt.Sprintf("/agents/%s/%s/chat/%s?share=%s", ns, name, sessionID, token)
	if c.uiURL != "" {
		return c.uiURL + path
	}
	return path
}

func (c *shareClient) callContext(ctx context.Context, userID string) (context.Context, context.CancelFunc) {
	return c.controllerClient.CallContext(ctx, userID)
}

func shareToMap(share *apiv1alpha1.SessionShare) map[string]any {
	createdAt := time.Time{}
	if share.GetCreatedAt() != nil {
		createdAt = share.GetCreatedAt().AsTime()
	}
	return map[string]any{
		"id":         share.GetId(),
		"token":      share.GetToken(),
		"session_id": share.GetSessionId(),
		"user_id":    share.GetUserId(),
		"read_only":  share.GetReadOnly(),
		"created_at": createdAt.Format(time.RFC3339Nano),
	}
}

func (c *shareClient) createShare(ctx context.Context, userID, sessionID string, readOnly *bool) (*apiv1alpha1.SessionShare, error) {
	callContext, cancel := c.callContext(ctx, userID)
	defer cancel()
	response, err := c.controllerClient.SessionService().CreateSessionShare(callContext, &apiv1alpha1.CreateSessionShareRequest{
		SessionId: sessionID,
		ReadOnly:  readOnly,
	})
	if err != nil {
		return nil, err
	}
	if response.GetShare() == nil {
		return nil, fmt.Errorf("response did not include a share")
	}
	return response.GetShare(), nil
}

func (c *shareClient) listShares(ctx context.Context, userID, sessionID string) ([]any, error) {
	callContext, cancel := c.callContext(ctx, userID)
	defer cancel()
	response, err := c.controllerClient.SessionService().ListSessionShares(callContext, &apiv1alpha1.ListSessionSharesRequest{SessionId: sessionID})
	if err != nil {
		return nil, err
	}
	shares := make([]any, 0, len(response.GetShares()))
	for _, share := range response.GetShares() {
		shares = append(shares, shareToMap(share))
	}
	return shares, nil
}

func (c *shareClient) deleteShare(ctx context.Context, userID, sessionID, token string) error {
	callContext, cancel := c.callContext(ctx, userID)
	defer cancel()
	_, err := c.controllerClient.SessionService().DeleteSessionShare(callContext, &apiv1alpha1.DeleteSessionShareRequest{
		SessionId: sessionID,
		Token:     token,
	})
	return err
}

type createShareInput struct {
	// ReadOnly controls whether the shared link allows visitors to send messages.
	// When nil (not provided by the model), the server defaults to true (read-only).
	ReadOnly *bool `json:"read_only,omitempty"`
}

// NewCreateShareLinkTool creates a tool that generates a share token for the current session.
func NewCreateShareLinkTool(controllerClient *controllerclient.Client, appName string) (tool.Tool, error) {
	if controllerClient == nil {
		return nil, fmt.Errorf("controller client is required")
	}
	c := &shareClient{
		controllerClient: controllerClient,
		uiURL:            strings.TrimRight(os.Getenv("KAGENT_UI_URL"), "/"),
		appName:          appName,
	}
	return functiontool.New(functiontool.Config{
		Name: "create_share_link",
		Description: "Creates a share link for the current chat session. " +
			"Returns a URL any authenticated user can open to view this session. " +
			"The link is read-only by default (visitors cannot send messages). " +
			"Set read_only=false to allow visitors to interact. " +
			"Each call creates a new token; existing tokens remain valid.",
	}, func(ctx agent.Context, in createShareInput) (map[string]any, error) {
		sessionID := ctx.SessionID()
		if sessionID == "" {
			return nil, fmt.Errorf("create_share_link: no session ID in context")
		}
		share, err := c.createShare(ctx, ctx.UserID(), sessionID, in.ReadOnly)
		if err != nil {
			return nil, fmt.Errorf("create_share_link: %w", err)
		}
		return map[string]any{
			"url":       c.shareURL(share.GetToken(), sessionID),
			"read_only": share.GetReadOnly(),
		}, nil
	})
}

// NewListShareLinksTool creates a tool that lists active share tokens for the current session.
func NewListShareLinksTool(controllerClient *controllerclient.Client, appName string) (tool.Tool, error) {
	if controllerClient == nil {
		return nil, fmt.Errorf("controller client is required")
	}
	c := &shareClient{
		controllerClient: controllerClient,
		uiURL:            strings.TrimRight(os.Getenv("KAGENT_UI_URL"), "/"),
		appName:          appName,
	}
	return functiontool.New(functiontool.Config{
		Name: "list_share_links",
		Description: "Lists all active share links for the current session. " +
			"Returns each share token and creation time. " +
			"Use this to find a token before calling delete_share_link.",
	}, func(ctx agent.Context, _ struct{}) (map[string]any, error) {
		sessionID := ctx.SessionID()
		if sessionID == "" {
			return nil, fmt.Errorf("list_share_links: no session ID in context")
		}
		shares, err := c.listShares(ctx, ctx.UserID(), sessionID)
		if err != nil {
			return nil, fmt.Errorf("list_share_links: %w", err)
		}
		return map[string]any{"shares": shares}, nil
	})
}

type deleteShareInput struct {
	Token string `json:"token"`
}

// NewDeleteShareLinkTool creates a tool that revokes a specific share token for the current session.
func NewDeleteShareLinkTool(controllerClient *controllerclient.Client, appName string) (tool.Tool, error) {
	if controllerClient == nil {
		return nil, fmt.Errorf("controller client is required")
	}
	c := &shareClient{
		controllerClient: controllerClient,
		uiURL:            strings.TrimRight(os.Getenv("KAGENT_UI_URL"), "/"),
		appName:          appName,
	}
	return functiontool.New(functiontool.Config{
		Name: "delete_share_link",
		Description: "Deletes a share link by token, immediately revoking access for anyone using it. " +
			"Use list_share_links first to find the token you want to revoke.",
	}, func(ctx agent.Context, in deleteShareInput) (map[string]any, error) {
		if in.Token == "" {
			return nil, fmt.Errorf("delete_share_link: token is required")
		}
		sessionID := ctx.SessionID()
		if sessionID == "" {
			return nil, fmt.Errorf("delete_share_link: no session ID in context")
		}
		if err := c.deleteShare(ctx, ctx.UserID(), sessionID, in.Token); err != nil {
			return nil, fmt.Errorf("delete_share_link: %w", err)
		}
		return map[string]any{"status": "revoked", "token": in.Token}, nil
	})
}
