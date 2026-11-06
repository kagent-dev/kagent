package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestCreateToolsetsDoesNotContactSlowServers(t *testing.T) {
	t.Run("Given a slow MCP server, when toolsets are constructed, assert that no request delays startup", func(t *testing.T) {
		var requests atomic.Int32
		server := newDiscoveryServer(t, 0, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			select {
			case <-time.After(500 * time.Millisecond):
			case <-r.Context().Done():
			}
		}))
		sets := CreateToolsets(t.Context(), []*configv1.ADKHTTPMCPServerConfig{{Params: &configv1.ADKHTTPConnectionParams{Url: server.URL}}}, nil, nil, false, nil)
		require.Len(t, sets, 1)
		require.Zero(t, requests.Load())
	})
}

func TestFailedClassificationIsOmittedUntilNextRun(t *testing.T) {
	// Given a transient discovery failure, when multiple model steps run, assert
	// that the server stays omitted until a new execution scope retries it.
	var unavailable atomic.Bool
	unavailable.Store(true)
	var lists atomic.Int32
	server := newDiscoveryServer(t, 0, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" {
			lists.Add(1)
			if unavailable.Load() {
				return nil, errors.New("catalog unavailable")
			}
		}
		return next(ctx, method, req)
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	ctx := testReadonlyContext{Context: WithDiscoveryScope(t.Context())}
	listed, err := ts.Tools(ctx)
	require.NoError(t, err)
	require.Empty(t, listed)
	require.EqualValues(t, 1, lists.Load(), "ADK must not discover after classification fails")
	unavailable.Store(false)
	listed, err = ts.Tools(ctx)
	require.NoError(t, err)
	require.Empty(t, listed)
	require.EqualValues(t, 1, lists.Load())

	// Same InvocationID, new run (including approval resumes).
	listed, err = ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
	require.Contains(t, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}), "c_app")
	// Another run's successful classification must not change the omitted run.
	listed, err = ts.Tools(ctx)
	require.NoError(t, err)
	require.Empty(t, listed)
	require.Empty(t, MCPAppToolNamesFromToolsets(ctx, []tool.Toolset{ts}), "another run's classification must not tag omitted tools in this run")
}

func TestClassificationIncludesEveryPageAndPreservesAllowlist(t *testing.T) {
	// Given one tool per page and a configured allowlist, when discovery runs,
	// assert that later app tools survive and app-only tools remain excluded.
	var lists atomic.Int32
	server := newDiscoveryServer(t, 1, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" {
			lists.Add(1)
			require.Contains(t, req.GetSession().(*mcpsdk.ServerSession).InitializeParams().Capabilities.Extensions, mcpUIExtensionName)
		}
		return next(ctx, method, req)
	}, nil)
	ts := discoveryToolset(t, server.URL, map[string]bool{"b_internal": true, "c_app": true})
	listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Equal(t, []string{"c_app"}, discoveryNames(listed))
	require.EqualValues(t, 3, lists.Load(), "classification must use ADK's own paginated discovery")
}

func TestPartialClassificationDoesNotPublishTools(t *testing.T) {
	// Given a later page fails, when discovery runs, assert that no partial
	// classification or unclassified ADK fallback becomes model-visible.
	var pages atomic.Int32
	server := newDiscoveryServer(t, 1, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" && pages.Add(1) == 2 {
			return nil, errors.New("second page unavailable")
		}
		return next(ctx, method, req)
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Empty(t, listed)
	require.Empty(t, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}))
	require.EqualValues(t, 2, pages.Load())
}

func TestClassificationWithoutScopeReturnsFailure(t *testing.T) {
	// Given a standalone caller, when classification fails, assert that an error
	// is returned without exposing ADK's unclassified catalog.
	var lists atomic.Int32
	server := newDiscoveryServer(t, 0, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" {
			lists.Add(1)
			return nil, errors.New("catalog unavailable")
		}
		return next(ctx, method, req)
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	listed, err := ts.Tools(testReadonlyContext{Context: t.Context()})
	require.ErrorContains(t, err, "catalog unavailable")
	require.Empty(t, listed)
	require.EqualValues(t, 1, lists.Load())
}

func TestDiscoveryFailureAfterSuccessfulDiscoveryIsOmittedUntilNextRun(t *testing.T) {
	// Given one successful discovery, when the next listing fails, assert that
	// omission lasts for that execution and a new execution can recover.
	var lists atomic.Int32
	server := newDiscoveryServer(t, 0, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" && lists.Add(1) == 2 {
			return nil, errors.New("ADK catalog unavailable")
		}
		return next(ctx, method, req)
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
	ctx := testReadonlyContext{Context: WithDiscoveryScope(t.Context())}
	listed, err = ts.Tools(ctx)
	require.NoError(t, err)
	require.Empty(t, listed)
	require.Empty(t, MCPAppToolNamesFromToolsets(ctx, []tool.Toolset{ts}))
	listed, err = ts.Tools(ctx)
	require.NoError(t, err)
	require.Empty(t, listed)
	require.EqualValues(t, 2, lists.Load(), "later model steps must not retry the omitted toolset")

	listed, err = ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
	require.EqualValues(t, 3, lists.Load())
	listed, err = ts.Tools(ctx)
	require.NoError(t, err)
	require.Empty(t, listed, "another execution's recovery must not undo omission")
	require.EqualValues(t, 3, lists.Load())
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = ts.Tools(testReadonlyContext{Context: canceled})
	require.ErrorIs(t, err, context.Canceled, "omission must not swallow cancellation")
}

func TestDiscoveryFailureWithoutScopePropagatesAfterSuccess(t *testing.T) {
	// Given a previously discovered standalone toolset, when its catalog fails,
	// assert that discovery returns the error and the next call can retry.
	var lists atomic.Int32
	server := newDiscoveryServer(t, 0, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" && lists.Add(1) == 2 {
			return nil, errors.New("standalone catalog unavailable")
		}
		return next(ctx, method, req)
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	ctx := testReadonlyContext{Context: t.Context()}
	listed, err := ts.Tools(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
	listed, err = ts.Tools(ctx)
	require.ErrorContains(t, err, "standalone catalog unavailable")
	require.Empty(t, listed)
	listed, err = ts.Tools(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
	require.EqualValues(t, 3, lists.Load())
}

func TestRepeatedPaginationCursorOmitsToolset(t *testing.T) {
	// Given pagination repeats a cursor, when classification runs, assert that
	// it stops and publishes neither a partial filter nor app metadata.
	var lists atomic.Int32
	var firstCursor string
	server := newDiscoveryServer(t, 1, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		result, err := next(ctx, method, req)
		if method == "tools/list" && err == nil {
			lists.Add(1)
			page := result.(*mcpsdk.ListToolsResult)
			if firstCursor == "" {
				firstCursor = page.NextCursor
			}
			page.NextCursor = firstCursor
		}
		return result, err
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Empty(t, listed)
	require.Empty(t, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}))
	require.EqualValues(t, 2, lists.Load())
}

func TestEmptyLegacyCatalogRemainsClassified(t *testing.T) {
	// Given an empty legacy catalog, when successive runs discover, assert that
	// each model step lists once and a successful empty catalog remains valid.
	var lists atomic.Int32
	server := newDiscoveryServer(t, 0, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		result, err := next(ctx, method, req)
		if method == "tools/list" && err == nil {
			lists.Add(1)
			result.(*mcpsdk.ListToolsResult).Tools = nil
		}
		return result, err
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	for range 2 {
		listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
		require.NoError(t, err)
		require.Empty(t, listed)
	}
	require.EqualValues(t, 2, lists.Load(), "one discovery per legacy-protocol model step")
}

func TestClassificationWaiterCanBeCanceled(t *testing.T) {
	// Given discovery is already in flight, when another invocation is canceled,
	// assert that it exits promptly and leaves the original discovery usable.
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := newDiscoveryServer(t, 0, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" {
			once.Do(func() { close(entered); <-release })
		}
		return next(ctx, method, req)
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	done := make(chan error, 1)
	go func() {
		_, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(WithDiscoveryScope(t.Context()))
	waiter := &observedReadonlyContext{testReadonlyContext: testReadonlyContext{Context: ctx}, waiting: make(chan struct{})}
	waiterDone := make(chan error, 1)
	go func() { _, err := ts.Tools(waiter); waiterDone <- err }()
	<-waiter.waiting
	cancel()
	require.ErrorIs(t, <-waiterDone, context.Canceled)
	close(release)
	require.NoError(t, <-done)
}

func TestCanceledClassificationCanRecover(t *testing.T) {
	// Given listing waits for cancellation, when the caller cancels discovery,
	// assert that cancellation propagates and a later run can recover.
	var canceled atomic.Bool
	entered := make(chan struct{})
	release := make(chan struct{})
	server := newDiscoveryServer(t, 0, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" && !canceled.Load() {
			close(entered)
			<-release
		}
		return next(ctx, method, req)
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	ctx, cancel := context.WithCancel(WithDiscoveryScope(t.Context()))
	done := make(chan error, 1)
	go func() { _, err := ts.Tools(testReadonlyContext{Context: ctx}); done <- err }()
	<-entered
	canceled.Store(true)
	cancel()
	close(release)
	require.ErrorIs(t, <-done, context.Canceled)
	listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
}

func TestSDKCachePreservesFilteredToolsAndAppMetadata(t *testing.T) {
	// Given a cacheable paginated catalog, when multiple executions prepare
	// tools, assert that cached pages remain filtered and keep app metadata.
	var lists, discoveries atomic.Int32
	server := newCacheableDiscoveryServer(1, 60000)
	url := serveDiscoveryServer(t, server, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" {
			lists.Add(1)
		}
		if method == "server/discover" {
			discoveries.Add(1)
		}
		return next(ctx, method, req)
	}, nil, true).URL
	ts := discoveryToolset(t, url, map[string]bool{"b_internal": true, "c_app": true})
	for range 3 {
		listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
		require.NoError(t, err)
		require.Equal(t, []string{"c_app"}, discoveryNames(listed))
		require.Equal(t, map[string]bool{"c_app": true}, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}))
	}
	require.EqualValues(t, 3, lists.Load(), "cached pages must not repeat discovery or bypass filtering")
	require.EqualValues(t, 1, discoveries.Load(), "classification must share ADK's connection")
}

func TestSDKCachePreservesEmptyFilteredCatalog(t *testing.T) {
	// Given only an app-only tool is allowed, when cached tools are requested,
	// assert that an empty filtered response remains empty without rediscovery.
	var lists atomic.Int32
	server := newCacheableDiscoveryServer(0, 60000)
	url := serveDiscoveryServer(t, server, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" {
			lists.Add(1)
		}
		return next(ctx, method, req)
	}, nil, true).URL
	ts := discoveryToolset(t, url, map[string]bool{"b_internal": true})
	for range 2 {
		listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
		require.NoError(t, err)
		require.Empty(t, listed)
	}
	require.EqualValues(t, 1, lists.Load())
}

func TestSDKCacheExpiryDiscoversNewTools(t *testing.T) {
	// Given a cacheable catalog without notifications, when its TTL expires,
	// assert that a newly advertised tool becomes visible through ADK discovery.
	var updated atomic.Bool
	var lists atomic.Int32
	server := newCacheableDiscoveryServer(0, 40)
	url := serveDiscoveryServer(t, server, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		result, err := next(ctx, method, req)
		if method == "tools/list" && err == nil {
			lists.Add(1)
			if updated.Load() {
				page := result.(*mcpsdk.ListToolsResult)
				page.Tools = append(page.Tools, &mcpsdk.Tool{Name: "d_new", InputSchema: map[string]any{"type": "object"}})
			}
		}
		return result, err
	}, nil, true).URL
	ts := discoveryToolset(t, url, nil)
	listed, err := ts.Tools(testReadonlyContext{Context: t.Context()})
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
	updated.Store(true)
	require.Eventually(t, func() bool {
		listed, err := ts.Tools(testReadonlyContext{Context: t.Context()})
		require.NoError(t, err)
		return slices.Contains(discoveryNames(listed), "d_new")
	}, time.Second, 10*time.Millisecond)
	require.GreaterOrEqual(t, lists.Load(), int32(2))
}

func TestSDKNotificationRefreshesVisibilityAndRetainsHistoricalAppNames(t *testing.T) {
	// Given a long-lived cache, when the catalog changes, assert that the SDK
	// refreshes visibility while retaining app names used to compact old results.
	server := newCacheableDiscoveryServer(0, 3600000)
	url := serveDiscoveryServer(t, server, nil, nil, true).URL
	ts := discoveryToolset(t, url, nil)
	listed, err := ts.Tools(testReadonlyContext{Context: t.Context()})
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "a_plain", Meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []string{"app"}}}}, discoveryToolHandler)
	server.RemoveTools("c_app")
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "d_new"}, discoveryToolHandler)
	require.Eventually(t, func() bool {
		listed, err := ts.Tools(testReadonlyContext{Context: t.Context()})
		require.NoError(t, err)
		return slices.Equal(discoveryNames(listed), []string{"d_new"})
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, map[string]bool{"c_app": true}, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}))
}

func TestPartialSDKCacheRetainsMetadataForLaterRecovery(t *testing.T) {
	// Given an app page is cached before a later page fails, when a new
	// execution recovers, assert that cached app metadata is still available.
	var unavailable atomic.Bool
	unavailable.Store(true)
	var firstPages atomic.Int32
	server := newCacheableDiscoveryServer(1, 60000)
	server.RemoveTools("a_plain")
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "a_app", Meta: mcpsdk.Meta{"ui": map[string]any{"resourceUri": "ui://first"}}}, discoveryToolHandler)
	url := serveDiscoveryServer(t, server, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" {
			if req.GetParams().(*mcpsdk.ListToolsParams).Cursor == "" {
				firstPages.Add(1)
			} else if unavailable.Load() {
				return nil, errors.New("later page unavailable")
			}
		}
		return next(ctx, method, req)
	}, nil, true).URL
	ts := discoveryToolset(t, url, nil)
	ctx := testReadonlyContext{Context: WithDiscoveryScope(t.Context())}
	listed, err := ts.Tools(ctx)
	require.NoError(t, err)
	require.Empty(t, listed)
	require.Empty(t, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}))
	unavailable.Store(false)
	listed, err = ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Equal(t, []string{"a_app", "c_app"}, discoveryNames(listed))
	require.Equal(t, map[string]bool{"a_app": true, "c_app": true}, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}))
	require.EqualValues(t, 1, firstPages.Load(), "the SDK must reuse its cached first page")
	listed, err = ts.Tools(ctx)
	require.NoError(t, err)
	require.Empty(t, listed, "recovery cannot undo another execution's omission")
}

func TestPaginationCycleThroughCachedPagesRespectsDiscoveryScope(t *testing.T) {
	// Given cached prefix pages, when an expired final page points back into
	// that prefix, assert that scoped discovery omits it and standalone discovery errors.
	var corrupt atomic.Bool
	var firstCursor string
	var firstPages atomic.Int32
	server := newCacheableDiscoveryServer(1, 60000)
	url := serveDiscoveryServer(t, server, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		result, err := next(ctx, method, req)
		if method == "tools/list" && err == nil {
			page := result.(*mcpsdk.ListToolsResult)
			if req.GetParams().(*mcpsdk.ListToolsParams).Cursor == "" {
				firstPages.Add(1)
				firstCursor = page.NextCursor
			}
			if page.NextCursor == "" {
				page.TTLMs = 0
				if corrupt.Load() {
					page.NextCursor = firstCursor
				}
			}
		}
		return result, err
	}, nil, true).URL
	ts := discoveryToolset(t, url, nil)
	listed, err := ts.Tools(testReadonlyContext{Context: t.Context()})
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
	corrupt.Store(true)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	listed, err = ts.Tools(testReadonlyContext{Context: ctx})
	require.ErrorContains(t, err, "repeated MCP pagination cursor")
	require.Empty(t, listed)
	require.EqualValues(t, 1, firstPages.Load(), "the malformed page must be reached through the cached prefix")
	scoped := testReadonlyContext{Context: WithDiscoveryScope(ctx)}
	listed, err = ts.Tools(scoped)
	require.NoError(t, err)
	require.Empty(t, listed)
	corrupt.Store(false)
	listed, err = ts.Tools(scoped)
	require.NoError(t, err)
	require.Empty(t, listed)
	listed, err = ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(ctx)})
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
}

func TestFailedDiscoveryDoesNotPublishStaleAppNamesAfterRecovery(t *testing.T) {
	// Given an app page followed by a failure, when the server changes that
	// tool to ordinary before recovery, assert that partial metadata is not published.
	var unavailable atomic.Bool
	unavailable.Store(true)
	server := newCacheableDiscoveryServer(1, 0)
	server.RemoveTools("a_plain")
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "a_app", Meta: mcpsdk.Meta{"ui": map[string]any{"resourceUri": "ui://first"}}}, discoveryToolHandler)
	url := serveDiscoveryServer(t, server, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		if method == "tools/list" && req.GetParams().(*mcpsdk.ListToolsParams).Cursor != "" && unavailable.Load() {
			return nil, errors.New("later page unavailable")
		}
		return next(ctx, method, req)
	}, nil, true).URL
	ts := discoveryToolset(t, url, nil)
	listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Empty(t, listed)
	require.Empty(t, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}))
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "a_app"}, discoveryToolHandler)
	unavailable.Store(false)
	listed, err = ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Equal(t, []string{"a_app", "c_app"}, discoveryNames(listed))
	require.Equal(t, map[string]bool{"c_app": true}, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}))
}

func TestSuccessfulDiscoveryClearsReclassifiedAppNames(t *testing.T) {
	// Given a published app tool, when a complete listing changes it to an
	// ordinary tool, assert that only removed app names remain for compaction.
	for _, protocol := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(protocol, func(t *testing.T) {
			var lists atomic.Int32
			server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "reclassification"}, &mcpsdk.ServerOptions{
				SupportedProtocolVersions: []string{protocol},
				SetCacheable:              func(_ context.Context, _ mcpsdk.Request, c *mcpsdk.Cacheable) { c.TTLMs = 60000 },
			})
			addDiscoveryTools(server)
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "removed_app", Meta: mcpsdk.Meta{"ui": map[string]any{"resourceUri": "ui://removed"}}}, discoveryToolHandler)
			url := serveDiscoveryServer(t, server, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
				if method == "tools/list" {
					lists.Add(1)
				}
				return next(ctx, method, req)
			}, nil, true).URL
			ts := discoveryToolset(t, url, nil)
			ctx := testReadonlyContext{Context: WithDiscoveryScope(t.Context())}
			_, err := ts.Tools(ctx)
			require.NoError(t, err)
			require.Equal(t, map[string]bool{"c_app": true, "removed_app": true}, MCPAppToolNamesFromToolsets(ctx, []tool.Toolset{ts}))

			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "c_app"}, discoveryToolHandler)
			server.RemoveTools("removed_app")
			require.Eventually(t, func() bool {
				listed, err := ts.Tools(ctx)
				require.NoError(t, err)
				return slices.Equal(discoveryNames(listed), []string{"a_plain", "c_app"}) &&
					!MCPAppToolNamesFromToolsets(ctx, []tool.Toolset{ts})["c_app"]
			}, time.Second, 10*time.Millisecond)
			require.Equal(t, map[string]bool{"removed_app": true}, MCPAppToolNamesFromToolsets(ctx, []tool.Toolset{ts}))
			before := lists.Load()
			_, err = ts.Tools(ctx)
			require.NoError(t, err)
			require.NotContains(t, MCPAppToolNamesFromToolsets(ctx, []tool.Toolset{ts}), "c_app")
			if protocol == "2026-07-28" {
				require.Equal(t, before, lists.Load(), "cached ordinary tools must not regain app classification")
			}
		})
	}
}

func TestDiscoveryPublicationWaiterCanBeCanceled(t *testing.T) {
	// Given an app listing awaits publication, when a concurrent invocation is
	// canceled after the catalog changes, assert that it cannot alter that listing.
	var ordinary atomic.Bool
	var lists atomic.Int32
	server := newDiscoveryServer(t, 0, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		result, err := next(ctx, method, req)
		if method == "tools/list" && err == nil {
			lists.Add(1)
			if ordinary.Load() {
				for _, candidate := range result.(*mcpsdk.ListToolsResult).Tools {
					if candidate.Name == "c_app" {
						candidate.Meta = nil
					}
				}
			}
		}
		return result, err
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	ts.inner = &publicationPausedToolset{Toolset: ts.inner, Closer: ts.inner.(io.Closer), entered: entered, release: release}
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFirst)
	done := make(chan error, 1)
	go func() { _, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())}); done <- err }()
	<-entered
	ordinary.Store(true)
	ctx, cancel := context.WithTimeout(WithDiscoveryScope(t.Context()), 100*time.Millisecond)
	defer cancel()
	listed, err := ts.Tools(testReadonlyContext{Context: ctx})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, listed)
	require.EqualValues(t, 1, lists.Load(), "waiting must cover publication, not just the network request")
	releaseFirst()
	require.NoError(t, <-done)
	require.Equal(t, map[string]bool{"c_app": true}, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}))
	listed, err = ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Equal(t, []string{"a_plain", "c_app"}, discoveryNames(listed))
	require.Empty(t, MCPAppToolNamesFromToolsets(t.Context(), []tool.Toolset{ts}))
}

func TestConcurrentDiscoveryKeepsEachResponseFiltered(t *testing.T) {
	// Given concurrent uncached listings with different visibility, when both
	// finish, assert that one response's classification never filters the other.
	entered, release := make(chan struct{}), make(chan struct{})
	var lists atomic.Int32
	server := newDiscoveryServer(t, 0, func(ctx context.Context, method string, req mcpsdk.Request, next mcpsdk.MethodHandler) (mcpsdk.Result, error) {
		result, err := next(ctx, method, req)
		if method == "tools/list" && err == nil {
			page := result.(*mcpsdk.ListToolsResult)
			if lists.Add(1) == 1 {
				close(entered)
				<-release
				page.Tools = []*mcpsdk.Tool{{Name: "same_name", InputSchema: map[string]any{"type": "object"}}}
			} else {
				page.Tools = []*mcpsdk.Tool{{Name: "same_name", InputSchema: map[string]any{"type": "object"}, Meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []string{"app"}}}}}
			}
		}
		return result, err
	}, nil)
	ts := discoveryToolset(t, server.URL, nil)
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFirst)
	done := make(chan []tool.Tool, 1)
	go func() {
		listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
		if err != nil {
			t.Error(err)
		}
		done <- listed
	}()
	<-entered
	waiter := &observedReadonlyContext{testReadonlyContext: testReadonlyContext{Context: WithDiscoveryScope(t.Context())}, waiting: make(chan struct{})}
	secondDone := make(chan []tool.Tool, 1)
	go func() {
		listed, err := ts.Tools(waiter)
		if err != nil {
			t.Error(err)
		}
		secondDone <- listed
	}()
	<-waiter.waiting
	releaseFirst()
	require.Equal(t, []string{"same_name"}, discoveryNames(<-done))
	require.Empty(t, <-secondDone)
}

func TestStdioDiscoveryStartsOnlyOneProcess(t *testing.T) {
	// Given a stdio MCP server, when construction and discovery run, assert
	// that startup starts no process and classification uses ADK's one process.
	marker := filepath.Join(t.TempDir(), "process-starts")
	ts, err := initializeToolSet(t.Context(), mcpServerParams{URL: "helper", ServerType: "stdio", Command: os.Args[0], Args: []string{"-test.run=^TestStdioDiscoveryHelper$"}, Env: map[string]string{"KAGENT_MCP_DISCOVERY_CHILD": marker}}, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := ts.inner.(io.Closer).Close(); err != nil && !strings.Contains(err.Error(), "signal: killed") {
			t.Errorf("close stdio: %v", err)
		}
	})
	_, err = os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist)
	listed, err := ts.Tools(testReadonlyContext{Context: WithDiscoveryScope(t.Context())})
	require.NoError(t, err)
	require.Equal(t, []string{"stdio_tool"}, discoveryNames(listed))
	content, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.Equal(t, "started\n", string(content))
}

func TestStdioDiscoveryHelper(t *testing.T) {
	marker := os.Getenv("KAGENT_MCP_DISCOVERY_CHILD")
	if marker == "" {
		t.Skip("subprocess helper")
	}
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(2)
	}
	if _, err := f.WriteString("started\n"); err != nil {
		os.Exit(3)
	}
	if err := f.Close(); err != nil {
		os.Exit(4)
	}
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "stdio-discovery"}, &mcpsdk.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "stdio_tool"}, discoveryToolHandler)
	if err := server.Run(context.Background(), &mcpsdk.StdioTransport{}); err != nil {
		os.Exit(5)
	}
	os.Exit(0)
}

type discoveryMiddleware func(context.Context, string, mcpsdk.Request, mcpsdk.MethodHandler) (mcpsdk.Result, error)

type publicationPausedToolset struct {
	tool.Toolset
	io.Closer
	entered chan struct{}
	release <-chan struct{}
	calls   atomic.Int32
}

func (p *publicationPausedToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	listed, err := p.Toolset.Tools(ctx)
	if p.calls.Add(1) == 1 {
		close(p.entered)
		<-p.release
	}
	return listed, err
}

type observedReadonlyContext struct {
	testReadonlyContext
	waiting chan struct{}
	once    sync.Once
}

func (o *observedReadonlyContext) Done() <-chan struct{} {
	o.once.Do(func() { close(o.waiting) })
	return o.Context.Done()
}

func newDiscoveryServer(t *testing.T, pageSize int, middleware discoveryMiddleware, intercept http.Handler) *httptest.Server {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "discovery-test", Version: "1"}, &mcpsdk.ServerOptions{PageSize: pageSize, SupportedProtocolVersions: []string{"2025-11-25"}})
	addDiscoveryTools(server)
	return serveDiscoveryServer(t, server, middleware, intercept, false)
}

func newCacheableDiscoveryServer(pageSize, ttl int) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "cached-discovery-test", Version: "1"}, &mcpsdk.ServerOptions{
		PageSize: pageSize, SupportedProtocolVersions: []string{"2026-07-28"},
		SetCacheable: func(_ context.Context, _ mcpsdk.Request, c *mcpsdk.Cacheable) { c.TTLMs = ttl },
	})
	addDiscoveryTools(server)
	return server
}

func addDiscoveryTools(server *mcpsdk.Server) {
	for _, descriptor := range []*mcpsdk.Tool{
		{Name: "a_plain"},
		{Name: "b_internal", Meta: mcpsdk.Meta{"ui": map[string]any{"visibility": []string{"app"}}}},
		{Name: "c_app", Meta: mcpsdk.Meta{"ui": map[string]any{"resourceUri": "ui://discovery"}}},
	} {
		mcpsdk.AddTool(server, descriptor, discoveryToolHandler)
	}
}

func discoveryToolHandler(context.Context, *mcpsdk.CallToolRequest, map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
	return nil, map[string]any{"result": "ok"}, nil
}

func serveDiscoveryServer(t *testing.T, server *mcpsdk.Server, middleware discoveryMiddleware, intercept http.Handler, stateless bool) *httptest.Server {
	t.Helper()
	if middleware != nil {
		server.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
			return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
				return middleware(ctx, method, req, next)
			}
		})
	}
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{Stateless: stateless})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if intercept != nil {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			intercept.ServeHTTP(w, r)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close() })
	return srv
}

func discoveryToolset(t *testing.T, url string, filter map[string]bool) *mcpAppToolset {
	t.Helper()
	ts, err := initializeToolSet(t.Context(), mcpServerParams{URL: url, ServerType: "http"}, filter)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ts.inner.(io.Closer).Close()) })
	return ts
}

func discoveryNames(tools []tool.Tool) []string {
	var names []string
	for _, t := range tools {
		names = append(names, t.Name())
	}
	slices.Sort(names)
	return names
}
