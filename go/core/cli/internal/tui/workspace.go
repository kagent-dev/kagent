package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kagent-dev/kagent/go/api/client"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
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
	// historyTaskLimit bounds how many past tasks open in the transcript.
	historyTaskLimit = 20
	// historyMessageLimit bounds the messages kept per historical task.
	historyMessageLimit = 20

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
}

// sessionLister narrows the client so tests can supply a fake.
type sessionLister interface {
	ListSessions(context.Context, *apiv1alpha1.ListSessionsRequest) (*apiv1alpha1.ListSessionsResponse, error)
}

// RunWorkspace launches the workspace: three cascading panels left, chat right.
func RunWorkspace(ctx context.Context, cfg Options, api *client.APIClientSet, gateway *client.GatewayClientSet, verbose bool) error {
	// A missing kubeconfig is not fatal; the reason is kept so panels can say why they fell back.
	kubeCatalog, catalogErr := newKubeCatalog()
	m := newWorkspaceModel(ctx, cfg, api, gateway, kubeCatalog, catalogErr, verbose)
	// Mouse reporting costs click-drag selection, which shift (option on macOS) restores.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

type sessionsLoadedMsg struct {
	sessions  []*apiv1alpha1.Session
	truncated bool
	err       error
}

type sessionSelectedMsg struct{ session *apiv1alpha1.Session }

// sessionHistoryLoadedMsg names its session, so a late reply cannot land in the new chat.
type sessionHistoryLoadedMsg struct {
	sessionID string
	tasks     []*a2atype.Task
	err       error
}

// catalogLoadedMsg carries Kubernetes names; on error the cascade falls back to session-derived ones.
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
	all           []*apiv1alpha1.Session
	catalogAgents []string
	current       *apiv1alpha1.Session
	status        string

	// Empty harness or agent means no filter; namespace is always set.
	namespace string
	agent     string

	focus panelID
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
	}
}

func (m *workspaceModel) Init() tea.Cmd {
	return tea.Batch(m.loadSessions(), m.loadCatalog(), m.loadNamespaces())
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

// loadCatalog reads the Agent names from Kubernetes.
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
			sessions  []*apiv1alpha1.Session
			pageToken string
		)
		for range maxSessionPages {
			response, err := m.lister.ListSessions(m.ctx, &apiv1alpha1.ListSessionsRequest{

				Page: &apiv1alpha1.PageRequest{Limit: sessionPageSize, PageToken: pageToken},
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
func (m *workspaceModel) loadHistory(session *apiv1alpha1.Session) tea.Cmd {
	id := session.GetId()
	return func() tea.Msg {
		a2aClient, err := m.client.A2A.ForAgent(m.ctx, session.GetAgent())
		if err != nil {
			return sessionHistoryLoadedMsg{sessionID: id, err: err}
		}
		historyLength := historyMessageLimit
		response, err := a2aClient.ListTasks(m.ctx, &a2atype.ListTasksRequest{
			ContextID:        session.GetId(),
			PageSize:         historyTaskLimit,
			HistoryLength:    &historyLength,
			IncludeArtifacts: true,
		})
		if err != nil {
			return sessionHistoryLoadedMsg{sessionID: id, err: err}
		}
		return sessionHistoryLoadedMsg{sessionID: id, tasks: response.Tasks}
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
	case clia2a.StreamResult, streamDoneMsg, spinner.TickMsg, tickMsg:
		if m.chat == nil {
			return m, nil
		}
		updated, cmd := m.chat.Update(msg)
		m.chat = updated.(*chatModel)
		return m, cmd
	}

	return m, m.forward(msg)
}

// handleMouse focuses the clicked panel and selects the clicked row.
func (m *workspaceModel) handleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
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
	if x >= sidebarWidth {
		if m.showDetails && x >= sidebarWidth+m.centerWidth() {
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

	// The API lists all sessions; this panel browses Kubernetes targets.
	m.all = slices.DeleteFunc(msg.sessions, func(session *apiv1alpha1.Session) bool {
		targetNamespace := session.GetAgent().GetNamespace()
		return targetNamespace != "" && targetNamespace != m.namespace
	})
	slices.SortStableFunc(m.all, func(a, b *apiv1alpha1.Session) int {
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
	m.agents.SetItems(countedNames(m.catalogAgents, m.all, func(i *apiv1alpha1.Session) string {
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
func countedNames(catalogNames []string, sessions []*apiv1alpha1.Session, name func(*apiv1alpha1.Session) string) []list.Item {
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
func (m *workspaceModel) visibleSessions() []*apiv1alpha1.Session {
	var kept []*apiv1alpha1.Session
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
func (m *workspaceModel) selectSession(session *apiv1alpha1.Session) tea.Cmd {
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
		// Cancel the in-flight stream before teardown rather than letting process exit drop it.
		m.chat.stop()
		return tea.Quit, true
	case "ctrl+r":
		return m.loadSessions(), true
	case "ctrl+d":
		m.showDetails = !m.showDetails
		return m.resize(), true
	case "tab":
		m.focus = m.focus.next()
		return m.resize(), true
	case "0", "1", "2", "3", "4":
		m.focus = panelID(msg.String()[0] - '0')
		return m.resize(), true
	case "enter":
		return m.activateFocused()
	}
	return nil, false
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
	inner := panelInnerWidth(sidebarWidth)
	for _, panel := range m.sidebarPanels() {
		panel.list.SetSize(inner, panelInnerHeight(panel.height))
		// Rows lay out to the panel's inner width, which only resize knows.
		panel.list.SetDelegate(rowDelegate{width: inner, row: panel.row})
	}

	if m.chat != nil {
		_, cmd := m.chat.Update(tea.WindowSizeMsg{
			Width:  panelInnerWidth(m.centerWidth()),
			Height: panelInnerHeight(available),
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

func (m *workspaceModel) centerWidth() int {
	width := m.width - sidebarWidth
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
	header := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorPrimary).Render(renderTitle())
	footer := m.footerView()
	available := m.bodyHeight()

	boxes := make([]string, 0, 4)
	for _, panel := range m.sidebarPanels() {
		boxes = append(boxes, panelBox(panel.id, panel.list.View(), sidebarWidth, panel.height, m.focus == panel.id))
	}
	sidebar := lipgloss.JoinVertical(lipgloss.Left, boxes...)

	parts := []string{
		sidebar,
		panelBox(panelChat, m.centerView(), m.centerWidth(), available, m.focus == panelChat),
	}
	if m.current != nil && m.showDetails {
		parts = append(parts, lipgloss.NewStyle().
			Width(detailsWidth).
			Padding(0, 1).
			Render(m.details))
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, lipgloss.JoinHorizontal(lipgloss.Top, parts...), footer)
}

func (m *workspaceModel) centerView() string {
	if m.chat != nil {
		return m.chat.View()
	}
	if len(m.all) == 0 {
		return "No Sessions.\n\nCreate one with:\nkagent create session --agent A"
	}
	if m.current != nil {
		return fmt.Sprintf("Session is %s.\n\nIt cannot accept messages right now.\nPress ctrl+r to refresh, or pick another in panel [3].",
			sessionview.StateLabel(m.current.GetState()))
	}
	return "Select a Session in panel [3] to start chatting."
}

// footerView is the keybinding hint bar, plus any current error.
func (m *workspaceModel) footerView() string {
	hints := theme.DimStyle().Render(
		"navigate: ↑↓  focus: click, tab or 0-4  search: /  enter: drill down, open  refresh: ctrl+r  details: ctrl+d  quit: ctrl+c")
	if m.status == "" {
		return hints
	}
	return lipgloss.JoinVertical(lipgloss.Left, theme.ErrorStyle().Render(m.status), hints)
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

// noCatalogErr explains why there is no catalog, preserving the kubeconfig failure.
func (m *workspaceModel) noCatalogErr() error {
	if m.catalogErr != nil {
		return m.catalogErr
	}
	return errors.New("no Kubernetes client configured")
}
