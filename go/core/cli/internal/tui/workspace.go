package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/kagent-dev/kagent/go/api/client"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	sessionview "github.com/kagent-dev/kagent/go/core/cli/internal/tui/session"
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/theme"
	"github.com/kagent-dev/kagent/go/core/internal/version"
)

const (
	// sessionPageSize matches the server's default list page.
	sessionPageSize = 50
	// maxSessionPages bounds the pages walked on open; reaching it is reported rather than hidden.
	maxSessionPages = 20
	// historyPageSize is how many past tasks one history request fetches, the task store's most.
	historyPageSize = 100

	sidebarWidth = 34
	detailsWidth = 32
	// Cascade panels size to their contents up to this cap; the session panel takes what is left.
	maxFilterPanelHeight = 9
	minFilterPanelHeight = 5
	// allNames is the synthetic row that clears a cascade filter.
	allNames = "(all)"
)

// Options contains the settings the workspace needs from the CLI's connection.
type Options struct {
	// Namespace is the connection's namespace; the browsing namespace changes, this one does not.
	Namespace string
	// HideSidebar starts the chat at full width, without the Namespaces, Agents and Sessions panels.
	HideSidebar bool
	// MuteBell stops the terminal bell that rings when a turn ends while the window is unfocused.
	MuteBell bool
}

// sessionLister narrows the client so tests can supply a fake.
type sessionLister interface {
	ListSessions(context.Context, *apiv1.ListSessionsRequest) (*apiv1.ListSessionsResponse, error)
}

// RunWorkspace launches the workspace: three cascading panels left, chat right.
func RunWorkspace(ctx context.Context, cfg Options, api *client.APIClientSet, gateway *client.GatewayClientSet, verbose bool) error {
	m := newWorkspaceModel(ctx, cfg, api, gateway, &apiCatalog{agents: api.Agent}, nil, verbose)
	// Mouse reporting costs click-drag selection, which shift (option on macOS) restores.
	// Focus reports tell the workspace when the user looks away, so a finished turn can ring the bell.
	options := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithReportFocus()}
	// Kitty keyboard reports give Shift+Enter its own code; Init asks for them once raw mode is on.
	if runtime.GOOS != "windows" && term.IsTerminal(int(os.Stdin.Fd())) {
		options = append(options, tea.WithInput(&kittyInput{File: os.Stdin}))
		m.kittyKeys = true
		defer fmt.Print(kittyPop)
	}
	_, err := tea.NewProgram(m, options...).Run()
	return err
}

type sessionsLoadedMsg struct {
	sessions  []*apiv1.Session
	truncated bool
	err       error
}

type sessionSelectedMsg struct{ session *apiv1.Session }

// sessionHistoryLoadedMsg names its session, so a late reply cannot land in the new chat.
type sessionHistoryLoadedMsg struct {
	sessionID string
	tasks     []*a2atype.Task
	err       error
}

// catalogLoadedMsg carries Agent names; on error the cascade falls back to session-derived ones.
type catalogLoadedMsg struct {
	agents []string
	err    error
}

// namespacesLoadedMsg carries namespaces holding Agents; a forbidden list leaves the current one.
type namespacesLoadedMsg struct {
	namespaces []namespaceCount
	err        error
}

type workspaceModel struct {
	// ctx cancels in-flight I/O; Bubble Tea commands take no context of their own.
	ctx        context.Context
	cfg        Options
	client     *client.GatewayClientSet
	lister     sessionLister
	catalog    catalog
	catalogErr error
	verbose    bool

	width  int
	height int

	// panels
	namespaces  list.Model
	agents      list.Model
	sessions    list.Model
	chat        *chatModel
	details     string
	showDetails bool

	// all holds every fetched Session; the cascade panels above narrow it.
	all           []*apiv1.Session
	catalogAgents []string
	current       *apiv1.Session
	status        string

	// Empty harness or agent means no filter; namespace is always set.
	namespace string
	agent     string

	focus panelID
	// sidebar shows the Namespaces, Agents and Sessions panels; /sidebar toggles it for the run.
	sidebar bool
	// away is set while the terminal reports it has lost focus; a terminal that
	// sends no focus reports leaves it false, so the bell never rings there.
	away bool
	// quitArmed is when ctrl+c last asked for a second press to exit.
	quitArmed time.Time
	// sizedBody is the body height the panels were last sized to.
	sizedBody int
	// screen is the last drawn view's rows. screenFrom and screenSel are a drag
	// that began outside the chat's rows, so it selects across the whole screen.
	screen     []string
	screenFrom *cell
	screenSel  *selection
	// kittyKeys is set when stdin is a terminal that may take the kitty keyboard protocol.
	kittyKeys bool
}

// newWorkspaceModel builds the model from resolved dependencies; it reads no configuration of its own.
func newWorkspaceModel(ctx context.Context, cfg Options, api *client.APIClientSet, gateway *client.GatewayClientSet, kubeCatalog catalog, catalogErr error, verbose bool) *workspaceModel {
	var lister sessionLister
	if api != nil {
		lister = api.Session
	}

	return &workspaceModel{
		ctx:        ctx,
		cfg:        cfg,
		client:     gateway,
		lister:     lister,
		catalog:    kubeCatalog,
		catalogErr: catalogErr,
		verbose:    verbose,
		// Seed the delegates so rows render sanely before the first resize.
		namespaces: newPanelList(rowDelegate{width: panelInnerWidth(sidebarWidth), row: nameRow}),
		agents:     newPanelList(rowDelegate{width: panelInnerWidth(sidebarWidth), row: nameRow}),
		sessions:   newPanelList(rowDelegate{width: panelInnerWidth(sidebarWidth), row: sessionRow}),
		namespace:  cfg.Namespace,
		focus:      panelSessions,
		sidebar:    !cfg.HideSidebar,
	}
}

func (m *workspaceModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadSessions(), m.loadCatalog(), m.loadNamespaces()}
	if m.kittyKeys {
		cmds = append(cmds, func() tea.Msg { fmt.Print(kittyPush); return nil })
	}
	return tea.Batch(cmds...)
}

// loadNamespaces lists the namespaces that hold Agents.
func (m *workspaceModel) loadNamespaces() tea.Cmd {
	return func() tea.Msg {
		if m.catalog == nil {
			return namespacesLoadedMsg{err: m.noCatalogErr()}
		}
		namespaces, err := m.catalog.Namespaces(m.ctx)
		return namespacesLoadedMsg{namespaces: namespaces, err: err}
	}
}

// loadCatalog reads the Agent names through the kagent API.
func (m *workspaceModel) loadCatalog() tea.Cmd {
	return func() tea.Msg {
		if m.catalog == nil {
			return catalogLoadedMsg{err: m.noCatalogErr()}
		}

		agents, err := m.catalog.Agents(m.ctx, m.namespace)
		if err != nil {
			return catalogLoadedMsg{err: err}
		}
		return catalogLoadedMsg{agents: agents}
	}
}

// loadSessions walks every page, bounded so a large deployment cannot stall startup.
func (m *workspaceModel) loadSessions() tea.Cmd {
	return func() tea.Msg {
		if m.lister == nil {
			return sessionsLoadedMsg{err: fmt.Errorf("no kagent client configured")}
		}

		var (
			sessions  []*apiv1.Session
			pageToken string
		)
		for range maxSessionPages {
			response, err := m.lister.ListSessions(m.ctx, &apiv1.ListSessionsRequest{

				Page: &apiv1.PageRequest{Limit: sessionPageSize, PageToken: pageToken},
			})
			if err != nil {
				return sessionsLoadedMsg{err: err}
			}
			sessions = append(sessions, response.GetSessions()...)
			pageToken = response.GetPage().GetNextPageToken()
			if pageToken == "" {
				return sessionsLoadedMsg{sessions: sessions}
			}
		}
		return sessionsLoadedMsg{sessions: sessions, truncated: true}
	}
}

// loadHistory preserves the durable task order returned by the gateway.
func (m *workspaceModel) loadHistory(session *apiv1.Session) tea.Cmd {
	id := session.GetId()
	return func() tea.Msg {
		a2aClient, err := m.client.A2A.ForAgent(m.ctx, session.GetAgent())
		if err != nil {
			return sessionHistoryLoadedMsg{sessionID: id, err: err}
		}
		tasks, err := allTasks(m.ctx, a2aClient.ListTasks, id)
		return sessionHistoryLoadedMsg{sessionID: id, tasks: tasks, err: err}
	}
}

// allTasks lists every task of a session, oldest first, each with its whole
// history; a HistoryLength would keep only each task's last messages.
func allTasks(ctx context.Context, list func(context.Context, *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error), contextID string) ([]*a2atype.Task, error) {
	var tasks []*a2atype.Task
	request := &a2atype.ListTasksRequest{ContextID: contextID, PageSize: historyPageSize, IncludeArtifacts: true}
	for {
		response, err := list(ctx, request)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, response.Tasks...)
		if response.NextPageToken == "" || response.NextPageToken == request.PageToken {
			return tasks, nil
		}
		request.PageToken = response.NextPageToken
	}
}

func (m *workspaceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.resize()

	case sessionsLoadedMsg:
		return m, m.applySessions(msg)

	case catalogLoadedMsg:
		// Without a catalog the panels still work, so this only warns.
		if msg.err != nil {
			m.status = fmt.Sprintf("Listing only Agents that have sessions: %v", msg.err)
			return m, nil
		}
		m.catalogAgents = msg.agents
		m.rebuildPanels()
		return m, m.resize()

	case namespacesLoadedMsg:
		m.applyNamespaces(msg)
		return m, m.resize()

	case sessionSelectedMsg:
		return m, m.selectSession(msg.session)

	case sessionHistoryLoadedMsg:
		if msg.sessionID != m.current.GetId() || m.chat == nil {
			return m, nil // the user selected something else while this was in flight
		}
		if msg.err != nil {
			m.status = fmt.Sprintf("Failed to load history: %v", msg.err)
			return m, nil
		}
		for _, task := range msg.tasks {
			m.chat.AppendHistoryTask(task)
		}
		m.chat.jumpToLatest() // a resumed session opens at its latest turn
		// Tasks are sorted oldest first, so the last is the most recent thing this session did.
		if last := len(msg.tasks) - 1; last >= 0 && msg.tasks[last] != nil && msg.tasks[last].Status.Timestamp != nil {
			m.chat.setHeaderMeta(stateBadge(m.current.GetState()), *msg.tasks[last].Status.Timestamp)
		}
		return m, nil

	case tea.KeyMsg:
		if cmd, handled := m.handleKey(msg); handled {
			return m, cmd
		}

	case tea.MouseMsg:
		if cmd, handled := m.handleMouse(msg); handled {
			return m, cmd
		}

	// Stream and timer messages go to the chat wherever focus is, or a reply is stranded.
	case streamMsg, streamDoneMsg, spinner.TickMsg, tickMsg, noticeDoneMsg, copiedMsg:
		if m.chat == nil {
			return m, nil
		}
		updated, cmd := m.chat.Update(msg)
		m.chat = updated.(*chatModel)
		m.ringIfAway()
		return m, cmd

	case toggleSidebarMsg:
		m.sidebar = !m.sidebar
		if !m.sidebar {
			m.focus = panelChat
		}
		return m, m.resize()

	case tea.BlurMsg:
		m.away = true
		return m, nil

	case tea.FocusMsg:
		m.away = false
		return m, nil
	}

	return m, m.forward(msg)
}

// handleMouse focuses the clicked panel and selects the clicked row.
func (m *workspaceModel) handleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if cmd, handled := m.screenMouse(msg); handled {
		return cmd, true
	}
	if cmd, handled := m.chatMouse(msg); handled {
		return cmd, true
	}
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}
	target, ok := m.panelAt(msg.X, msg.Y)
	if !ok {
		return nil, false
	}
	m.focus = target

	panel, top := m.panelListAt(target)
	if panel == nil {
		return m.resize(), true
	}
	// Rows start below the panel's top border and title line.
	if row := msg.Y - top - 2; row >= 0 {
		index := panel.Paginator.Page*panel.Paginator.PerPage + row
		if index < len(panel.Items()) {
			panel.Select(index)
			m.syncCascade() // clicking a row filters, exactly as moving to it does
		}
	}
	return m.resize(), true
}

// screenMouse selects by dragging from anywhere outside the chat's own rows:
// the title line, panel titles, the sidebar and the footer. A press there that never moves stays a click.
func (m *workspaceModel) screenMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	here := cell{msg.X, msg.Y}
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		m.screenSel, m.screenFrom = nil, nil
		if m.inChatRows(msg.X, msg.Y) {
			return nil, false
		}
		m.screenFrom = &here
		if m.chat != nil {
			m.chat.selection, m.chat.dragFrom = nil, nil
		}
		return nil, true
	case msg.Action == tea.MouseActionMotion && m.screenFrom != nil:
		if here != *m.screenFrom {
			m.screenSel = &selection{from: *m.screenFrom, to: here}
		}
		return nil, true
	case msg.Action == tea.MouseActionRelease && m.screenFrom != nil:
		m.screenFrom = nil
		if m.screenSel == nil {
			return nil, false // a click
		}
		m.screenSel.to = here
		return m.copyScreen(), true
	}
	return nil, false
}

// inChatRows reports whether a screen cell is one of the chat's own drawn rows, where the chat selects.
func (m *workspaceModel) inChatRows(x, y int) bool {
	if m.chat == nil {
		return false
	}
	if target, ok := m.panelAt(x, y); !ok || target != panelChat {
		return false
	}
	ox, oy := m.chatOrigin()
	cx, cy := x-ox, y-oy
	return cx >= 0 && cx < m.chatWidth() && cy >= 0 && cy < len(m.chat.geo.view)
}

// copyScreen copies the whole-screen selection, reporting through the chat's notice.
func (m *workspaceModel) copyScreen() tea.Cmd {
	text := screenText(m.screen, *m.screenSel, m.width)
	if text == "" {
		return nil
	}
	return func() tea.Msg {
		return copiedMsg{chars: utf8.RuneCountInString(text), err: copyText(text)}
	}
}

// chatMouse passes a left-button event inside the chat panel to the chat, in the chat's own cells.
func (m *workspaceModel) chatMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if m.chat == nil || (msg.Button != tea.MouseButtonLeft && msg.Action != tea.MouseActionMotion) {
		return nil, false
	}
	// A drag that began in the chat stays the chat's wherever it moves or ends.
	dragging := m.chat.dragFrom != nil && msg.Action != tea.MouseActionPress
	if target, ok := m.panelAt(msg.X, msg.Y); !dragging && (!ok || target != panelChat) {
		return nil, false
	}
	ox, oy := m.chatOrigin()
	x, y := msg.X-ox, msg.Y-oy
	if dragging {
		// Clamp to the transcript only when the drag began there, so the input and header stay selectable.
		geo := m.chat.geo
		lo, hi := 0, max(len(geo.view)-1, 0)
		if from := m.chat.dragFrom.y; from >= geo.top && from < geo.top+geo.rows {
			lo, hi = geo.top, max(geo.top+geo.rows-1, geo.top)
		}
		x, y = min(max(x, 0), m.chatWidth()-1), min(max(y, lo), hi)
	}
	if msg.Action != tea.MouseActionRelease {
		return m.chat.mouse(msg, x, y), true
	}
	m.focus = panelChat
	return tea.Batch(m.resize(), m.chat.mouse(msg, x, y)), true
}

// chatOrigin is the screen cell of the chat's top-left cell, inside the border and padding.
func (m *workspaceModel) chatOrigin() (int, int) {
	if m.chatBoxed() {
		return m.sidebarCols() + 2, lineCount(renderTitle()) + 1
	}
	return 1, lineCount(renderTitle())
}

// chatBoxed reports whether an open chat sits in a bordered panel: only beside the sidebar.
func (m *workspaceModel) chatBoxed() bool {
	return m.chat == nil || m.showSidebar()
}

// chatWidth and chatHeight are the chat's own cells: inside its box, or the
// full width less a one-column margin each side when it has none.
func (m *workspaceModel) chatWidth() int {
	if m.chatBoxed() {
		return panelInnerWidth(m.centerWidth())
	}
	return max(m.centerWidth()-2, 8)
}

func (m *workspaceModel) chatHeight(available int) int {
	if m.chatBoxed() {
		return max(available-2, 1) // inside the border; the chat draws its own title row
	}
	return available
}

// sidebarPanel is one stacked list panel, its drawn height, and how its rows render.
type sidebarPanel struct {
	id     panelID
	list   *list.Model
	height int
	row    func(list.Item, int) string
}

// sidebarPanels is the single source of sidebar geometry; the session panel takes what is left.
func (m *workspaceModel) sidebarPanels() []sidebarPanel {
	namespaces := filterPanelHeight(m.namespaces)
	agents := filterPanelHeight(m.agents)
	return []sidebarPanel{
		{panelNamespaces, &m.namespaces, namespaces, nameRow},
		{panelAgents, &m.agents, agents, nameRow},
		{panelSessions, &m.sessions, max(m.bodyHeight()-namespaces-agents, 3), sessionRow},
	}
}

// panelAt maps a screen cell to the panel drawn there.
func (m *workspaceModel) panelAt(x, y int) (panelID, bool) {
	header := lineCount(renderTitle())
	if y < header || y >= header+m.bodyHeight() {
		return 0, false
	}
	if x >= m.sidebarCols() {
		if m.showDetails && x >= m.sidebarCols()+m.centerWidth() {
			return 0, false // the details pane is not focusable
		}
		return panelChat, true
	}
	row := y - header
	for _, panel := range m.sidebarPanels() {
		if row < panel.height {
			return panel.id, true
		}
		row -= panel.height
	}
	return panelSessions, true
}

// panelListAt returns a panel's list and the screen row of its top border.
func (m *workspaceModel) panelListAt(id panelID) (*list.Model, int) {
	top := lineCount(renderTitle())
	for _, panel := range m.sidebarPanels() {
		if panel.id == id {
			return panel.list, top
		}
		top += panel.height
	}
	return nil, 0
}

// applySessions stores the fetched sessions and rebuilds every panel.
func (m *workspaceModel) applySessions(msg sessionsLoadedMsg) tea.Cmd {
	if msg.err != nil {
		m.status = fmt.Sprintf("Failed to load Sessions: %v", msg.err)
		return nil
	}
	m.status = ""
	if msg.truncated {
		m.status = fmt.Sprintf("Showing the first %d Sessions; more pages are available.", len(msg.sessions))
	}

	// The API lists all sessions; this panel browses one namespace.
	m.all = slices.DeleteFunc(msg.sessions, func(session *apiv1.Session) bool {
		targetNamespace := session.GetAgent().GetNamespace()
		return targetNamespace != "" && targetNamespace != m.namespace
	})
	slices.SortStableFunc(m.all, func(a, b *apiv1.Session) int {
		return b.GetCreatedAt().AsTime().Compare(a.GetCreatedAt().AsTime()) // newest first
	})
	m.rebuildPanels()

	if m.current == nil {
		return m.openSelectedSession()
	}
	// A refresh may have deleted the open session or changed its state, so re-read it.
	for _, session := range m.all {
		if session.GetId() == m.current.GetId() {
			if session.GetState() != m.current.GetState() {
				return m.selectSession(session)
			}
			m.current = session
			m.renderDetails()
			return nil
		}
	}
	m.chat.stop()
	m.chat, m.current = nil, nil
	m.status = "The open Session no longer exists."
	return m.openSelectedSession()
}

// rebuildPanels derives the cascade from fetched sessions, then narrows by the current selections.
func (m *workspaceModel) rebuildPanels() {
	m.rebuildAgents()
}

// rebuildAgents also rebuilds the sessions below it, since SetItems resets this panel's cursor.
func (m *workspaceModel) rebuildAgents() {
	m.agents.SetItems(countedNames(m.catalogAgents, m.all, func(i *apiv1.Session) string {
		return i.GetAgent().GetName()
	}))
	m.agent = ""
	m.rebuildSessions()
}

func (m *workspaceModel) rebuildSessions() {
	visible := m.visibleSessions()
	items := make([]list.Item, 0, len(visible))
	for _, session := range visible {
		items = append(items, sessionItem{Session: session})
	}
	m.sessions.SetItems(items)
}

// applyNamespaces fills the namespace panel, always including the current namespace.
func (m *workspaceModel) applyNamespaces(msg namespacesLoadedMsg) {
	namespaces := msg.namespaces
	if msg.err != nil {
		m.status = fmt.Sprintf("Listing only the current namespace: %v", msg.err)
		namespaces = nil
	}
	if !slices.ContainsFunc(namespaces, func(n namespaceCount) bool { return n.Name == m.namespace }) {
		namespaces = append(namespaces, namespaceCount{Name: m.namespace})
		slices.SortFunc(namespaces, func(a, b namespaceCount) int { return strings.Compare(a.Name, b.Name) })
	}

	items := make([]list.Item, 0, len(namespaces))
	for _, namespace := range namespaces {
		items = append(items, nameItem{name: namespace.Name, count: namespace.Agents})
	}
	m.namespaces.SetItems(items)
	for i, namespace := range namespaces {
		if namespace.Name == m.namespace {
			m.namespaces.Select(i)
		}
	}
}

// syncCascade applies the cascade cursors; a target namespace change reloads its catalog and filters the session list.
func (m *workspaceModel) syncCascade() tea.Cmd {
	if namespace := selectedNamespace(m.namespaces); namespace != "" && namespace != m.namespace {
		m.namespace = namespace
		m.agent = ""
		m.all, m.catalogAgents = nil, nil
		m.current, m.chat = nil, nil
		m.rebuildPanels()
		return tea.Batch(m.loadSessions(), m.loadCatalog())
	}
	if agent := selectedName(m.agents); agent != m.agent {
		m.agent = agent
		m.rebuildSessions()
	}
	return nil
}

// selectedNamespace reads the namespace panel's cursor; unlike the filters it has no "(all)" row.
func selectedNamespace(panel list.Model) string {
	item, ok := panel.SelectedItem().(nameItem)
	if !ok {
		return ""
	}
	return item.name
}

// countedNames lists catalog names with session counts after an "(all)" row, including unused ones.
func countedNames(catalogNames []string, sessions []*apiv1.Session, name func(*apiv1.Session) string) []list.Item {
	counts := map[string]int{}
	for _, value := range catalogNames {
		if value != "" {
			counts[value] = 0
		}
	}
	for _, session := range sessions {
		if value := name(session); value != "" {
			counts[value]++
		}
	}

	order := make([]string, 0, len(counts))
	for value := range counts {
		order = append(order, value)
	}
	slices.Sort(order)

	items := make([]list.Item, 0, len(order)+1)
	items = append(items, nameItem{name: allNames, count: len(sessions)})
	for _, value := range order {
		items = append(items, nameItem{name: value, count: counts[value]})
	}
	return items
}

// visibleSessions applies both cascade filters.
func (m *workspaceModel) visibleSessions() []*apiv1.Session {
	var kept []*apiv1.Session
	for _, session := range m.all {
		if m.agent == "" || session.GetAgent().GetName() == m.agent {
			kept = append(kept, session)
		}
	}
	return kept
}

// openSelectedSession opens whatever the session panel currently highlights.
func (m *workspaceModel) openSelectedSession() tea.Cmd {
	item, ok := m.sessions.SelectedItem().(sessionItem)
	if !ok {
		return nil
	}
	selected := item.Session
	return func() tea.Msg { return sessionSelectedMsg{session: selected} }
}

// selectSession opens a chat; a non-READY session is not dialed at all.
func (m *workspaceModel) selectSession(session *apiv1.Session) tea.Cmd {
	if session == nil {
		return nil
	}
	m.chat.stop() // otherwise its stream delivers into the next session's chat
	m.current = session
	m.renderDetails()

	if !sessionview.Ready(session) {
		m.chat = nil
		m.status = fmt.Sprintf("Session is %s and cannot accept messages.", sessionview.StateLabel(session.GetState()))
		return nil
	}

	m.status = ""
	a2aClient, err := m.client.A2A.ForAgent(m.ctx, session.GetAgent())
	if err != nil {
		m.chat = nil
		m.status = fmt.Sprintf("Failed to connect to Session: %v", err)
		return nil
	}

	send := func(ctx context.Context, req *a2atype.SendMessageRequest) <-chan clia2a.StreamResult {
		return clia2a.StreamToChannel(ctx, a2aClient, req)
	}
	m.chat = newChatModel(m.ctx, session.GetAgent().GetName(), session.GetId(), send, m.verbose)
	m.chat.setHeaderMeta(stateBadge(session.GetState()), session.GetUpdatedAt().AsTime())
	if !m.sidebar {
		m.focus = panelChat // the sidebar hides once a chat opens, so the chat is the only panel left
	}
	// Bubble Tea calls Init only on the root model, so start the chat's here.
	return tea.Batch(m.chat.Init(), m.resize(), m.loadHistory(session))
}

// handleKey reports whether it consumed the key; the rest fall through to the focused panel.
func (m *workspaceModel) handleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	// An open filter input owns every key, including the panel digits.
	if m.filtering() {
		return nil, false
	}

	switch msg.String() {
	case "ctrl+c":
		// With text selected, ctrl+c copies it once, as in a terminal; the next press cancels or quits.
		if m.screenSel != nil {
			cmd := m.copyScreen()
			m.screenSel = nil
			return cmd, true
		}
		if m.chat.hasSelection() {
			cmd := m.chat.copySelection()
			m.chat.selection = nil
			return cmd, true
		}
		if m.chat == nil || time.Since(m.quitArmed) < noticeTTL {
			// Cancel the in-flight stream before teardown rather than letting process exit drop it.
			m.chat.stop()
			return tea.Quit, true
		}
		// The first press cancels a running turn and arms exit for a second press.
		m.chat.cancelTurn()
		m.quitArmed = time.Now()
		notice := "Press ctrl+c again to exit"
		if n := len(m.chat.queue); n > 0 {
			notice += " · " + plural(n, "queued message") + " will be dropped"
		}
		return m.chat.showNotice(notice), true
	case "esc":
		if m.screenSel != nil {
			m.screenSel = nil
			return nil, true
		}
		if !m.chat.hasSelection() {
			return nil, false
		}
		m.chat.selection = nil
		return nil, true
	case "ctrl+r":
		return m.loadSessions(), true
	case "ctrl+x":
		if m.chat == nil {
			return nil, false
		}
		m.chat.toggleTools()
		return nil, true
	case "ctrl+d":
		m.showDetails = !m.showDetails
		return m.resize(), true
	case "tab":
		if !m.showSidebar() {
			return nil, true // the chat is the only panel showing
		}
		m.focus = m.focus.next()
		return m.resize(), true
	case "0", "1", "2", "3", "4":
		// In the chat a digit is text, so a message can hold numbers.
		if m.focus == panelChat && m.chat != nil {
			return nil, false
		}
		m.focus = panelID(msg.String()[0] - '0')
		return m.resize(), true
	case "enter":
		return m.activateFocused()
	}
	return nil, false
}

// ringBell sounds the terminal bell, which macOS terminals show as a dock bounce or badge; tests replace it.
var ringBell = func() { _, _ = fmt.Fprint(os.Stdout, "\a") }

// ringIfAway rings the bell when a turn finished or began waiting on the user while the terminal was unfocused.
func (m *workspaceModel) ringIfAway() {
	if m.chat == nil || !m.chat.attention {
		return
	}
	m.chat.attention = false
	if m.away && !m.cfg.MuteBell {
		ringBell()
	}
}

// activateFocused narrows the cascade, or opens a chat from the session panel.
func (m *workspaceModel) activateFocused() (tea.Cmd, bool) {
	switch m.focus {
	case panelNamespaces, panelAgents:
		// The cursor already applied the filter, so enter just drills down.
		m.focus = m.focus.next()
		return m.resize(), true
	case panelSessions:
		return m.openSelectedSession(), true
	default:
		return nil, false
	}
}

// selectedName maps the "(all)" row back to an empty filter.
func selectedName(panel list.Model) string {
	item, ok := panel.SelectedItem().(nameItem)
	if !ok || item.name == allNames {
		return ""
	}
	return item.name
}

// filtering reports whether a panel's filter input is currently open.
func (m *workspaceModel) filtering() bool {
	return slices.ContainsFunc(m.sidebarPanels(), func(panel sidebarPanel) bool {
		return panel.list.FilterState() == list.Filtering
	})
}

// forward routes a message to the focused panel.
func (m *workspaceModel) forward(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.focus {
	case panelNamespaces:
		m.namespaces, cmd = m.namespaces.Update(msg)
		cmd = tea.Batch(cmd, m.syncCascade())
	case panelAgents:
		m.agents, cmd = m.agents.Update(msg)
		cmd = tea.Batch(cmd, m.syncCascade())
	case panelSessions:
		m.sessions, cmd = m.sessions.Update(msg)
	default:
		if m.chat != nil {
			updated, chatCmd := m.chat.Update(msg)
			m.chat = updated.(*chatModel)
			cmd = chatCmd
		}
	}
	return cmd
}

func (m *workspaceModel) resize() tea.Cmd {
	if m.width == 0 || m.height == 0 {
		return nil
	}
	available := m.bodyHeight()
	m.sizedBody = available
	inner := panelInnerWidth(sidebarWidth)
	for _, panel := range m.sidebarPanels() {
		panel.list.SetSize(inner, panelInnerHeight(panel.height))
		// Rows lay out to the panel's inner width, which only resize knows.
		panel.list.SetDelegate(rowDelegate{width: inner, row: panel.row})
	}

	if m.chat != nil {
		_, cmd := m.chat.Update(tea.WindowSizeMsg{
			Width:  m.chatWidth(),
			Height: m.chatHeight(available),
		})
		return cmd
	}
	return nil
}

// filterPanelHeight sizes a cascade panel to its rows; the extra line is bubbles' own spacing.
func filterPanelHeight(panel list.Model) int {
	const listSpacing = 1
	return min(max(len(panel.Items())+panelChromeHeight+listSpacing, minFilterPanelHeight), maxFilterPanelHeight)
}

func (m *workspaceModel) bodyHeight() int {
	return max(m.height-lineCount(renderTitle())-lineCount(m.footerView()), 1)
}

// showSidebar reports whether the sidebar draws. Without an open chat there is no
// input to type /sidebar into, so the panels show regardless.
func (m *workspaceModel) showSidebar() bool {
	return m.sidebar || m.chat == nil
}

// sidebarCols is how wide the sidebar draws: none while hidden.
func (m *workspaceModel) sidebarCols() int {
	if m.showSidebar() {
		return sidebarWidth
	}
	return 0
}

func (m *workspaceModel) centerWidth() int {
	width := m.width - m.sidebarCols()
	if m.showDetails {
		width -= detailsWidth
	}
	return max(width, 20)
}

// renderDetails fills the right pane with the selected session's identity.
func (m *workspaceModel) renderDetails() {
	if m.current == nil {
		m.details = ""
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ID\n%s\n\n", m.current.GetId())
	fmt.Fprintf(&b, "Agent\n%s\n\n", m.current.GetAgent().GetName())
	fmt.Fprintf(&b, "State\n%s\n\n", sessionview.StateLabel(m.current.GetState()))

	if failure := m.current.GetFailure(); failure != nil {
		fmt.Fprintf(&b, "\nFailure\n%s\n", failure.GetMessage())
	}
	m.details = b.String()
}

func (m *workspaceModel) View() string {
	// A status line that came or went changes the footer's height, so the body resizes to match.
	if m.bodyHeight() != m.sizedBody {
		m.resize()
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorPrimary).Render(renderTitle())
	footer := m.footerView()
	available := m.bodyHeight()

	var parts []string
	if m.showSidebar() {
		boxes := make([]string, 0, 4)
		for _, panel := range m.sidebarPanels() {
			boxes = append(boxes, panelBox(panel.id, panel.list.View(), sidebarWidth, panel.height, m.focus == panel.id))
		}
		parts = append(parts, lipgloss.JoinVertical(lipgloss.Left, boxes...))
	}
	parts = append(parts, m.chatPanel(available))
	if m.current != nil && m.showDetails {
		parts = append(parts, lipgloss.NewStyle().
			Width(detailsWidth).
			Padding(0, 1).
			Render(m.details))
	}

	view := lipgloss.JoinVertical(lipgloss.Left, header, lipgloss.JoinHorizontal(lipgloss.Top, parts...), footer)
	m.screen = fitScreen(strings.Split(view, "\n"), m.width, m.height)
	return strings.Join(highlightRows(slices.Clone(m.screen), m.screenSel, m.width), "\n")
}

// fitScreen makes the frame exactly the window's size, so the terminal never
// scrolls: too narrow a window cuts rows, too short a one loses the top rows first.
func fitScreen(rows []string, width, height int) []string {
	if width <= 0 || height <= 0 {
		return rows
	}
	for i, row := range rows {
		if ansi.StringWidth(row) > width {
			rows[i] = ansi.Truncate(row, width, "")
		}
	}
	if len(rows) > height {
		return rows[len(rows)-height:]
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	return rows
}

// chatPanel boxes the center pane. An open chat's own header row is its title, so it has no separate title row.
func (m *workspaceModel) chatPanel(available int) string {
	if m.chat == nil {
		return panelBox(panelChat, m.centerView(), m.centerWidth(), available, m.focus == panelChat)
	}
	if !m.chatBoxed() {
		return lipgloss.NewStyle().Width(m.centerWidth()).Height(available).Padding(0, 1).Render(m.centerView())
	}
	return untitledBox(m.centerView(), m.centerWidth(), available, m.focus == panelChat)
}

func (m *workspaceModel) centerView() string {
	if m.chat != nil {
		m.chat.unfocused = m.focus != panelChat
		m.chat.open = !m.chatBoxed()
		return m.chat.View()
	}
	if len(m.all) == 0 {
		return "No Sessions.\n\nCreate one with:\nkagent agent session create --agent A"
	}
	if m.current != nil {
		return fmt.Sprintf("Session is %s.\n\nIt cannot accept messages right now.\nPress ctrl+r to refresh, or pick another in panel [3].",
			sessionview.StateLabel(m.current.GetState()))
	}
	return "Select a Session in panel [3] to start chatting."
}

// footerView is the keybinding hint bar, plus any current error.
func (m *workspaceModel) footerView() string {
	// Shift+Enter needs the kitty keyboard protocol; without it, the fallbacks are named.
	newline := "newline: alt+enter or ctrl+j"
	if kittyKeys.Load() {
		newline = "newline: shift+enter"
	}
	line := "navigate: ↑↓  focus: click, tab or 0-4  search: /  enter: drill down, open  " + newline + "  sidebar: /sidebar  refresh: ctrl+r  details: ctrl+d  tool output: ctrl+x  quit: ctrl+c"
	if !m.showSidebar() {
		// Only the chat shows, so the panel hints would point at nothing.
		line = newline + "  sidebar: /sidebar  details: ctrl+d  tool output: ctrl+x  quit: ctrl+c"
	}
	status := m.status
	if m.width > 0 {
		// A line wider than the window would wrap in the terminal and push the frame past its height.
		line, status = packHints(strings.Split(line, "  "), m.width), ansi.Wrap(status, m.width, "")
	}
	hints := theme.DimStyle().Render(line)
	if status == "" {
		return hints
	}
	return lipgloss.JoinVertical(lipgloss.Left, theme.ErrorStyle().Render(status), hints)
}

// packHints joins hints two spaces apart, starting a new line rather than splitting one.
func packHints(hints []string, width int) string {
	var lines []string
	for _, hint := range hints {
		if last := len(lines) - 1; last >= 0 && ansi.StringWidth(lines[last])+2+ansi.StringWidth(hint) <= width {
			lines[last] += "  " + hint
		} else {
			lines = append(lines, hint)
		}
	}
	return strings.Join(lines, "\n")
}

// renderTitle returns the styled header line.
func renderTitle() string {
	return fmt.Sprintf("kagent  %s", lipgloss.NewStyle().Foreground(theme.ColorMuted).Render(version.GetVersion()))
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// noCatalogErr explains why there is no catalog, preserving the construction failure.
func (m *workspaceModel) noCatalogErr() error {
	if m.catalogErr != nil {
		return m.catalogErr
	}
	return errors.New("no Agent catalog configured")
}
