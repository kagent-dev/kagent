package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	tea "github.com/charmbracelet/bubbletea"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var testTime = time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

// fakeLister serves one canned page per call, so paging is observable.
type fakeLister struct {
	pages    []*apiv1alpha1.ListSessionsResponse
	err      error
	requests []*apiv1alpha1.ListSessionsRequest
}

func (f *fakeLister) ListSessions(_ context.Context, request *apiv1alpha1.ListSessionsRequest) (*apiv1alpha1.ListSessionsResponse, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return nil, f.err
	}
	return f.pages[min(len(f.requests)-1, len(f.pages)-1)], nil
}

// fakeCatalog stands in for the Kubernetes read.
type fakeCatalog struct {
	namespaces []namespaceCount
	agents     []string
	err        error
}

func (f *fakeCatalog) Namespaces(context.Context) ([]namespaceCount, error) {
	return f.namespaces, f.err
}
func (f *fakeCatalog) Agents(context.Context, string) ([]string, error) {
	return f.agents, f.err
}

func testWorkspace(t *testing.T, lister sessionLister) *workspaceModel {
	t.Helper()
	conn := connection.DefaultOptions()
	conn.Namespace = "kagent"
	conn.Timeout = 30 * time.Second
	api, err := conn.APIClient()
	require.NoError(t, err)
	t.Cleanup(func() { _ = api.Close() })
	gateway, err := conn.GatewayClient()
	require.NoError(t, err)
	t.Cleanup(func() { _ = gateway.Close() })

	m := newWorkspaceModel(t.Context(), Options{Namespace: conn.Namespace}, api, gateway, nil, nil, false)
	m.lister = lister
	m.width, m.height = 120, 40
	return m
}

func workspaceSession(id, template string, state apiv1alpha1.SessionState, created time.Time) *apiv1alpha1.Session {
	return &apiv1alpha1.Session{
		Id: id,

		Agent: &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: template},

		State:     state,
		CreatedAt: timestamppb.New(created),
	}
}

func readySession(id, template string) *apiv1alpha1.Session {
	return workspaceSession(id, template, apiv1alpha1.SessionState_SESSION_STATE_READY, testTime)
}

func page(nextToken string, sessions ...*apiv1alpha1.Session) *apiv1alpha1.ListSessionsResponse {
	return &apiv1alpha1.ListSessionsResponse{
		Sessions: sessions,
		Page:     &apiv1alpha1.PageResponse{NextPageToken: nextToken},
	}
}

// loaded runs the session fetch and applies it, returning the follow-up command.
func loaded(m *workspaceModel) tea.Cmd {
	return m.applySessions(m.loadSessions()().(sessionsLoadedMsg))
}

// runBatch executes a command's messages, flattening one level of batching.
func runBatch(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, batched := range batch {
			batched()
		}
	}
}

func TestWorkspaceLoadsSessions(t *testing.T) {
	one := readySession("a", "smoke")
	tests := []struct {
		name          string
		lister        *fakeLister
		wantSessions  int
		wantRequests  int
		wantTruncated bool
		wantStatus    string
	}{
		{
			name: "follows the next page token",
			lister: &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{
				page("token-2", one), page("", readySession("b", "reporter")),
			}},
			wantSessions: 2, wantRequests: 2,
		},
		{
			name:         "stops at the page bound rather than looping",
			lister:       &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("more", one)}},
			wantSessions: maxSessionPages, wantRequests: maxSessionPages,
			wantTruncated: true, wantStatus: "more pages are available",
		},
		{
			name:       "reports a failure",
			lister:     &fakeLister{err: errors.New("unavailable")},
			wantStatus: "Failed to load Sessions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testWorkspace(t, tt.lister)
			msg := m.loadSessions()().(sessionsLoadedMsg)
			m.applySessions(msg)

			assert.Len(t, msg.sessions, tt.wantSessions)
			assert.Equal(t, tt.wantTruncated, msg.truncated)
			if tt.wantRequests > 0 {
				assert.Len(t, tt.lister.requests, tt.wantRequests)
			}
			if tt.wantStatus == "" {
				assert.Empty(t, m.status)
			} else {
				assert.Contains(t, m.status, tt.wantStatus)
			}
		})
	}
}

func TestWorkspaceSortsNewestFirstAndOpensOne(t *testing.T) {
	older := workspaceSession("old", "a", apiv1alpha1.SessionState_SESSION_STATE_READY, testTime.Add(-time.Hour))
	newer := readySession("new", "b")
	m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("", older, newer)}})

	cmd := loaded(m)

	require.Len(t, m.all, 2)
	assert.Equal(t, "new", m.all[0].GetId(), "newest sorts first")
	require.NotNil(t, cmd, "the first session opens automatically")
	assert.Equal(t, newer, cmd().(sessionSelectedMsg).session)
}

func TestWorkspaceSelectSession(t *testing.T) {
	tests := []struct {
		name       string
		state      apiv1alpha1.SessionState
		wantChat   bool
		wantStatus string
	}{
		{name: "ready opens a chat", state: apiv1alpha1.SessionState_SESSION_STATE_READY, wantChat: true},
		// The gateway rejects every call for a non-READY session, so the workspace must not dial one.
		{name: "suspended is not dialed", state: apiv1alpha1.SessionState_SESSION_STATE_SUSPENDED, wantStatus: "SUSPENDED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := workspaceSession("44444444-4444-4444-4444-444444444444", "reporter", tt.state, testTime)
			m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("", session)}})
			loaded(m)

			cmd := m.selectSession(session)

			if tt.wantChat {
				require.NotNil(t, m.chat)
				assert.Equal(t, session.GetId(), m.chat.contextID)
				assert.NotNil(t, cmd, "history loads for a READY session")
				return
			}
			assert.Nil(t, m.chat)
			assert.Nil(t, cmd, "a non-READY session loads no history")
			assert.Contains(t, m.status, tt.wantStatus)
			assert.Contains(t, m.View(), tt.wantStatus)
		})
	}
}

// Moving the cursor filters immediately; enter only drills down.
func TestWorkspaceCascadeFiltersOnCursorMove(t *testing.T) {
	m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{
		page("", readySession("a", "smoke"), readySession("c", "reporter")),
	}})
	loaded(m)

	// Every distinct template gets a row, after "(all)".
	require.Len(t, m.agents.Items(), 3)
	assert.Equal(t, nameItem{name: allNames, count: 2}, m.agents.Items()[0])
	require.Len(t, m.sessions.Items(), 2)

	m.focus = panelAgents
	m.forward(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, "reporter", m.agent)
	require.Len(t, m.sessions.Items(), 1)
	assert.Equal(t, "c", m.sessions.Items()[0].(sessionItem).GetId())

	m.forward(tea.KeyMsg{Type: tea.KeyUp}) // back to "(all)"
	assert.Empty(t, m.agent)
	assert.Len(t, m.sessions.Items(), 2)
}

func TestWorkspaceKeys(t *testing.T) {
	tests := []struct {
		name       string
		focus      panelID
		key        tea.KeyMsg
		wantFocus  panelID
		wantReload bool
	}{
		{
			name: "a digit focuses its panel", focus: panelChat,
			key:       tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")},
			wantFocus: panelAgents,
		},
		{
			name: "tab cycles forward", focus: panelSessions,
			key: tea.KeyMsg{Type: tea.KeyTab}, wantFocus: panelChat,
		},
		{
			name: "enter drills down a cascade panel", focus: panelNamespaces,
			key: tea.KeyMsg{Type: tea.KeyEnter}, wantFocus: panelAgents,
		},
		{
			name: "ctrl+r reloads", focus: panelChat,
			key: tea.KeyMsg{Type: tea.KeyCtrlR}, wantFocus: panelChat, wantReload: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("")}})
			m.focus = tt.focus

			cmd, handled := m.handleKey(tt.key)

			require.True(t, handled, "the key must be consumed")
			assert.Equal(t, tt.wantFocus, m.focus)
			if tt.wantReload {
				require.NotNil(t, cmd)
				assert.IsType(t, sessionsLoadedMsg{}, cmd())
			}
		})
	}
}

func TestWorkspaceMouse(t *testing.T) {
	// Offsets are from the session panel's top border: +2 is its first row.
	tests := []struct {
		name        string
		x, rowBelow int
		action      tea.MouseAction
		wantHandled bool
		wantFocus   panelID
		wantIndex   int
	}{
		{
			name: "a row click focuses the panel and selects that row",
			x:    4, rowBelow: 3, action: tea.MouseActionRelease,
			wantHandled: true, wantFocus: panelSessions, wantIndex: 1,
		},
		{
			name: "a chat click focuses the chat",
			x:    sidebarWidth + 5, rowBelow: 1, action: tea.MouseActionRelease,
			wantHandled: true, wantFocus: panelChat,
		},
		{
			name: "motion is not a click",
			x:    4, rowBelow: 3, action: tea.MouseActionMotion,
			wantFocus: panelSessions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{
				page("", readySession("a", "smoke"), readySession("b", "reporter")),
			}})
			loaded(m)
			m.resize()
			m.focus = panelSessions
			_, top := m.panelListAt(panelSessions)

			_, handled := m.handleMouse(tea.MouseMsg{
				X: tt.x, Y: top + tt.rowBelow, Action: tt.action, Button: tea.MouseButtonLeft,
			})

			assert.Equal(t, tt.wantHandled, handled)
			assert.Equal(t, tt.wantFocus, m.focus)
			assert.Equal(t, tt.wantIndex, m.sessions.Index())
		})
	}
}

func TestWorkspaceCatalog(t *testing.T) {
	tests := []struct {
		name       string
		catalog    catalog
		wantNames  map[string]int
		wantStatus string
	}{
		{
			name: "an unused template is listed at zero",
			catalog: &fakeCatalog{
				agents: []string{"smoke", "reporter"},
			},
			wantNames: map[string]int{allNames: 1, "smoke": 1, "reporter": 0},
		},
		{
			// Without a kubeconfig the cascade still works, from session data.
			name:       "falls back to session names",
			catalog:    &fakeCatalog{err: errors.New("no kubeconfig")},
			wantNames:  map[string]int{allNames: 1, "smoke": 1},
			wantStatus: "only Agents that have sessions",
		},
		{
			name:       "a nil catalog is not fatal",
			catalog:    nil,
			wantNames:  map[string]int{allNames: 1, "smoke": 1},
			wantStatus: "no Kubernetes client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{
				page("", readySession("a", "smoke")),
			}})
			m.catalog = tt.catalog
			loaded(m)

			m.Update(m.loadCatalog()())

			got := map[string]int{}
			for _, item := range m.agents.Items() {
				row := item.(nameItem)
				got[row.name] = row.count
			}
			assert.Equal(t, tt.wantNames, got)
			if tt.wantStatus != "" {
				assert.Contains(t, m.status, tt.wantStatus)
			}
		})
	}
}

func TestWorkspaceNamespacePanel(t *testing.T) {
	tests := []struct {
		name       string
		catalog    *fakeCatalog
		want       []nameItem
		wantStatus string
	}{
		{
			name: "lists namespaces with template counts",
			catalog: &fakeCatalog{namespaces: []namespaceCount{
				{Name: "kagent", Agents: 3}, {Name: "team-b", Agents: 1},
			}},
			want: []nameItem{{name: "kagent", count: 3}, {name: "team-b", count: 1}},
		},
		{
			// The panel must show where the user is even when the cluster-wide list was refused.
			name:       "always lists the current namespace",
			catalog:    &fakeCatalog{err: errors.New("forbidden")},
			want:       []nameItem{{name: "kagent"}},
			wantStatus: "only the current namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("")}})
			m.catalog = tt.catalog

			m.Update(m.loadNamespaces()())

			got := make([]nameItem, 0, len(m.namespaces.Items()))
			for _, item := range m.namespaces.Items() {
				got = append(got, item.(nameItem))
			}
			assert.Equal(t, tt.want, got)
			assert.Equal(t, 0, m.namespaces.Index(), "the current namespace starts selected")
			if tt.wantStatus != "" {
				assert.Contains(t, m.status, tt.wantStatus)
			}
		})
	}
}

func TestWorkspaceSwitchingNamespaceRefetches(t *testing.T) {
	lister := &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{
		page("", readySession("a", "smoke")),
	}}
	m := testWorkspace(t, lister)
	m.catalog = &fakeCatalog{namespaces: []namespaceCount{{Name: "kagent"}, {Name: "team-b"}}}
	m.Update(m.loadNamespaces()())
	loaded(m)
	require.Len(t, m.all, 1)

	m.focus = panelNamespaces
	cmd := m.forward(tea.KeyMsg{Type: tea.KeyDown})

	assert.Equal(t, "team-b", m.namespace)
	assert.Empty(t, m.all, "the previous namespace's sessions are cleared")
	assert.Nil(t, m.chat, "the open chat belonged to the old namespace")
	// The port-forward was established before the TUI started, so switching must not disturb it.
	assert.Equal(t, "kagent", m.cfg.Namespace, "the connection's namespace is untouched")

	runBatch(cmd)
	assert.NotEmpty(t, lister.requests)
}

// The delegate renders nothing it cannot type-assert, so these assert on output not model state.
func TestWorkspaceRenders(t *testing.T) {
	const id = "66666666-6666-6666-6666-666666666666"
	tests := []struct {
		name     string
		sessions []*apiv1alpha1.Session
		render   func(*workspaceModel) string
		want     []string
	}{
		{
			name:     "rows carry template, short ID, and a state glyph",
			sessions: []*apiv1alpha1.Session{readySession(id, "smoke")},
			render:   func(m *workspaceModel) string { return m.sessions.View() },
			want:     []string{"smoke", "66666666", "●"},
		},
		{
			name:   "an empty namespace says how to create a session",
			render: func(m *workspaceModel) string { return m.View() },
			want:   []string{"No Sessions", "create session"},
		},
		{
			name:     "details keep the full copyable ID",
			sessions: []*apiv1alpha1.Session{readySession(id, "reporter")},
			render: func(m *workspaceModel) string {
				m.current = m.all[0]
				m.renderDetails()
				return m.details
			},
			want: []string{id, "reporter", "READY"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("", tt.sessions...)}})
			loaded(m)
			m.resize()

			got := tt.render(m)
			for _, want := range tt.want {
				assert.Contains(t, got, want)
			}
		})
	}
}

// Async replies name their session, so a late one must not land in the new chat.
func TestWorkspaceIgnoresHistoryForAnotherSession(t *testing.T) {
	first, second := readySession("a", "smoke"), readySession("b", "reporter")
	m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("", first, second)}})
	loaded(m)
	m.selectSession(second)
	before := transcript(m.chat)

	m.Update(sessionHistoryLoadedMsg{
		sessionID: first.GetId(),
		tasks:     []*a2atype.Task{{ID: "t", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}},
	})

	assert.Equal(t, before, transcript(m.chat), "history for another session is dropped")
}

func TestWorkspaceStopsTheOutgoingStreamOnSwitch(t *testing.T) {
	first, second := readySession("a", "smoke"), readySession("b", "reporter")
	m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("", first, second)}})
	loaded(m)
	m.selectSession(first)
	stopped := false
	m.chat.cancel = func() { stopped = true }

	m.selectSession(second)

	assert.True(t, stopped, "the previous session's stream is cancelled")
}

// A chat streams under the workspace's context, so cancelling the program cancels the request.
func TestChatStreamsUnderTheWorkspaceContext(t *testing.T) {
	ready := readySession("a", "smoke")
	ctx, cancel := context.WithCancel(context.Background())
	m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("", ready)}})
	m.ctx = ctx
	loaded(m)
	m.selectSession(ready)

	streamCtx := make(chan context.Context, 1)
	m.chat.send = func(ctx context.Context, _ *a2atype.SendMessageRequest) <-chan clia2a.StreamResult {
		streamCtx <- ctx
		return make(chan clia2a.StreamResult)
	}
	m.chat.submit("hello")

	var sent context.Context
	select {
	case sent = <-streamCtx:
	default:
		t.Fatal("the chat never started a stream")
	}

	cancel()
	select {
	case <-sent.Done():
		assert.ErrorIs(t, sent.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancelling the workspace did not cancel the stream")
	}
}

// Stream messages must reach the chat even when a panel has focus, or the reply is stranded.
func TestWorkspaceRoutesStreamMessagesRegardlessOfFocus(t *testing.T) {
	ready := readySession("a", "smoke")
	m := testWorkspace(t, &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("", ready)}})
	loaded(m)
	m.selectSession(ready)
	m.focus = panelSessions

	m.Update(clia2a.StreamResult{Err: errors.New("stream disconnected")})

	assert.Contains(t, transcript(m.chat), "Connection error")
}

func TestWorkspaceRefreshDropsADeletedSession(t *testing.T) {
	ready := readySession("a", "smoke")
	lister := &fakeLister{pages: []*apiv1alpha1.ListSessionsResponse{page("", ready), page("")}}
	m := testWorkspace(t, lister)
	loaded(m)
	m.selectSession(ready)
	require.NotNil(t, m.chat)

	loaded(m) // second page is empty: the session is gone

	assert.Nil(t, m.chat, "a deleted session leaves no chat behind")
	assert.Nil(t, m.current)
	assert.Contains(t, m.status, "no longer exists")
}
