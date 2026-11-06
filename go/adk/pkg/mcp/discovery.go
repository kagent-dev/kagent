package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/semaphore"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type discoveryScopeKey struct{}

// WithDiscoveryScope remembers unavailable MCP toolsets for one runner.Run or
// A2A execution, including all its model steps and sub-agent calls. Start a new
// scope for each execution, even when an approval resume reuses InvocationID.
// The scope contains no connections and needs no explicit cleanup.
func WithDiscoveryScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, discoveryScopeKey{}, &discoveryScope{omitted: make(map[*mcpAppToolset]bool)})
}

type discoveryScope struct {
	mu      sync.Mutex
	omitted map[*mcpAppToolset]bool
}

func (d *discoveryScope) isOmitted(ts *mcpAppToolset) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.omitted[ts]
}

func (d *discoveryScope) omit(ts *mcpAppToolset) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.omitted[ts] = true
}

// App names are retained independently of the current catalog so existing
// model-result compaction continues to recognize historical app results.
// Visibility is enforced on each raw tools/list response, never by this map.
type mcpAppDiscovery struct {
	mu             sync.Mutex
	listedAppNames map[string]bool
	appToolNames   map[string]bool
}

type mcpAppToolset struct {
	inner     tool.Toolset
	discovery *mcpAppDiscovery
	listingMu *semaphore.Weighted
}

var _ tool.Toolset = (*mcpAppToolset)(nil)

func (m *mcpAppToolset) Name() string { return m.inner.Name() }

func (m *mcpAppToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, _ := ctx.Value(discoveryScopeKey{}).(*discoveryScope)
	if scope != nil && scope.isOmitted(m) {
		return nil, nil
	}
	// Keep raw classification and publication together, including SDK cache
	// hits. Waiters must be able to cancel without interrupting the listing.
	if err := m.listingMu.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer m.listingMu.Release(1)
	if scope != nil && scope.isOmitted(m) {
		return nil, nil
	}
	tools, err := m.inner.Tools(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if scope == nil {
			return nil, err
		}
		scope.omit(m)
		logging.FromContext(ctx).WarnContext(ctx, "failed to discover MCP tools; omitting toolset for this invocation", "error", err)
		return nil, nil
	}
	m.discovery.mu.Lock()
	for _, t := range tools {
		if m.discovery.listedAppNames[t.Name()] {
			m.discovery.appToolNames[t.Name()] = true
		} else {
			delete(m.discovery.appToolNames, t.Name())
		}
	}
	m.discovery.mu.Unlock()
	if scope != nil && scope.isOmitted(m) {
		return nil, nil
	}
	return tools, nil
}

// MCPAppToolNamesFromToolsets returns successfully discovered MCP App-capable
// tool names, including names retained for historical model-result compaction.
// Call it during model preparation, after Tools has run.
func MCPAppToolNamesFromToolsets(ctx context.Context, toolsets []tool.Toolset) map[string]bool {
	out := make(map[string]bool)
	scope, _ := ctx.Value(discoveryScopeKey{}).(*discoveryScope)
	for _, ts := range toolsets {
		aware, ok := ts.(*mcpAppToolset)
		if !ok || scope != nil && scope.isOmitted(aware) {
			continue
		}
		aware.discovery.mu.Lock()
		for name := range aware.discovery.appToolNames {
			out[name] = true
		}
		aware.discovery.mu.Unlock()
	}
	return out
}

func newMCPAppClient(configuredFilter map[string]bool, discovery *mcpAppDiscovery) *mcpsdk.Client {
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "kagent-adk"}, &mcpsdk.ClientOptions{
		Capabilities: mcpUIClientCapabilities(),
		// New-protocol servers subscribe only when a handler is configured.
		// The SDK itself invalidates its list cache on these notifications.
		ToolListChangedHandler: func(context.Context, *mcpsdk.ToolListChangedRequest) {},
	})
	client.AddSendingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method != "tools/list" {
				return next(ctx, method, req)
			}
			params := req.GetParams().(*mcpsdk.ListToolsParams)
			cursor, err := decodeToolCursor(params.Cursor)
			if err != nil {
				return nil, err
			}
			// Keep the SDK's cache key intact; only the wire request uses the
			// server's original opaque cursor.
			wireParams := *params
			wireParams.Cursor = cursor.Cursor
			wireReq := &mcpsdk.ClientRequest[*mcpsdk.ListToolsParams]{Session: req.GetSession().(*mcpsdk.ClientSession), Params: &wireParams}
			result, err := next(ctx, method, wireReq)
			if err != nil {
				return nil, err
			}
			page, ok := result.(*mcpsdk.ListToolsResult)
			if !ok || page == nil {
				return nil, fmt.Errorf("invalid MCP tools/list result")
			}
			if page.NextCursor != "" {
				page.NextCursor, err = cursor.next(page.NextCursor)
				if err != nil {
					return nil, err
				}
			}
			visible := make([]*mcpsdk.Tool, 0, len(page.Tools))
			discovery.mu.Lock()
			for _, t := range page.Tools {
				if t == nil || t.Name == "" || len(configuredFilter) > 0 && !configuredFilter[t.Name] {
					continue
				}
				switch mcpToolKindOf(t.Meta) {
				case mcpToolKindAppInternal:
					continue
				case mcpToolKindApp:
					discovery.listedAppNames[t.Name] = true
				default:
					delete(discovery.listedAppNames, t.Name)
				}
				visible = append(visible, t)
			}
			discovery.mu.Unlock()
			page.Tools = visible
			// ListTools caches this already-filtered response after middleware
			// returns. Preserve its TTL and all other response metadata.
			return page, nil
		}
	})
	return client
}

// ADK does not detect pagination cycles, and SDK cache hits bypass middleware.
// Carry the visited path in ADK's opaque cursor so a cached prefix participates
// in cycle detection too. Only the original server cursor goes on the wire.
type toolCursor struct {
	Cursor  string
	Visited []string
}

func decodeToolCursor(encoded string) (toolCursor, error) {
	if encoded == "" {
		return toolCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return toolCursor{}, fmt.Errorf("failed to decode MCP pagination cursor: %w", err)
	}
	var cursor toolCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return toolCursor{}, fmt.Errorf("failed to decode MCP pagination cursor: %w", err)
	}
	return cursor, nil
}

func (c toolCursor) next(cursor string) (string, error) {
	if cursor == c.Cursor || slices.Contains(c.Visited, cursor) {
		return "", fmt.Errorf("repeated MCP pagination cursor")
	}
	data, err := json.Marshal(toolCursor{Cursor: cursor, Visited: append(c.Visited, c.Cursor)})
	if err != nil {
		return "", fmt.Errorf("failed to encode MCP pagination cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
