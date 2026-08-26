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
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/instance"
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/theme"
	"github.com/kagent-dev/kagent/go/core/internal/version"
)

const (
	// instancePageSize matches the server's default list page.
	instancePageSize = 50
	// maxInstancePages bounds the pages walked on open so a large deployment
	// cannot stall startup. Reaching it is reported rather than hidden.
	maxInstancePages = 20
	// historyTaskLimit bounds how many past tasks open in the transcript.
	historyTaskLimit = 20
	// historyMessageLimit bounds the messages kept per historical task.
	historyMessageLimit = 20

	sidebarWidth = 34
	detailsWidth = 32
	// The harness and template panels size to their contents up to this cap;
	// the instance panel takes whatever height is left.
	maxFilterPanelHeight = 9
	minFilterPanelHeight = 5
	// allNames is the synthetic row that clears a cascade filter.
	allNames = "(all)"
)

// Options contains the settings the workspace needs from the CLI's connection.
type Options struct {
	// Namespace is the namespace the connection was established against. The
	// browsing namespace changes as the user navigates; this one does not.
	Namespace string
}

// instanceLister narrows the client so tests can supply a fake.
type instanceLister interface {
	ListAgentInstances(context.Context, *apiv1alpha1.ListAgentInstancesRequest) (*apiv1alpha1.ListAgentInstancesResponse, error)
}

// RunWorkspace launches the workspace: three cascading panels left, chat right.
// Mouse reporting costs click-drag selection, which shift (option on macOS) restores.
func RunWorkspace(ctx context.Context, cfg Options, clientSet *client.ClientSet, verbose bool) error {
	m := newWorkspaceModel(ctx, cfg, clientSet, verbose)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

type instancesLoadedMsg struct {
	instances []*apiv1alpha1.AgentInstance
	truncated bool
	err       error
}

type instanceSelectedMsg struct{ agentInstance *apiv1alpha1.AgentInstance }

// instanceHistoryLoadedMsg names the instance it belongs to: a reply that
// arrives after the user moved on must not land in the new chat.
type instanceHistoryLoadedMsg struct {
	instanceID string
	tasks      []*a2atype.Task
	err        error
}

// catalogLoadedMsg carries the Kubernetes-side names. An error is reported but
// not fatal: the cascade falls back to instance-derived names.
type catalogLoadedMsg struct {
	harnesses []string
	templates []string
	err       error
}

// namespacesLoadedMsg carries the namespaces holding AgentTemplates. Listing
// cluster-wide can be forbidden, so a failure leaves just the current one.
type namespacesLoadedMsg struct {
	namespaces []namespaceCount
	err        error
}

type workspaceModel struct {
	// ctx cancels in-flight I/O when the program stops. Bubble Tea commands
	// take no context of their own, so they close over this one.
	ctx        context.Context
	cfg        Options
	client     *client.ClientSet
	lister     instanceLister
	catalog    catalog
	catalogErr error
	verbose    bool

	width  int
	height int

	// panels
	namespaces  list.Model
	harnesses   list.Model
	templates   list.Model
	instances   list.Model
	chat        *chatModel
	details     string
	showDetails bool

	// data. all holds every fetched AgentInstance; the panels above narrow it.
	// The catalog names come from Kubernetes and include what has no instance.
	all              []*apiv1alpha1.AgentInstance
	catalogHarnesses []string
	catalogTemplates []string
	current          *apiv1alpha1.AgentInstance
	status           string

	// cascade selections. Empty harness or template means no filter; namespace
	// is always set, since every request is namespace-scoped.
	namespace string
	harness   string
	template  string

	focus panelID
}

func newWorkspaceModel(ctx context.Context, cfg Options, clientSet *client.ClientSet, verbose bool) *workspaceModel {
	var lister instanceLister
	if clientSet != nil {
		lister = clientSet.AgentInstance
	}
	// A missing kubeconfig is not fatal, but the reason is kept so the panels
	// can say why they fell back to names derived from the instances.
	kubeCatalog, catalogErr := newKubeCatalog()

	return &workspaceModel{
		ctx:        ctx,
		cfg:        cfg,
		client:     clientSet,
		lister:     lister,
		catalog:    kubeCatalog,
		catalogErr: catalogErr,
		verbose:    verbose,
		// Seed the delegates so rows render sanely before the first resize.
		namespaces: newPanelList(rowDelegate{width: panelInnerWidth(sidebarWidth), row: nameRow}),
		harnesses:  newPanelList(rowDelegate{width: panelInnerWidth(sidebarWidth), row: nameRow}),
		templates:  newPanelList(rowDelegate{width: panelInnerWidth(sidebarWidth), row: nameRow}),
		instances:  newPanelList(rowDelegate{width: panelInnerWidth(sidebarWidth), row: instanceRow}),
		namespace:  cfg.Namespace,
		focus:      panelInstances,
	}
}

func (m *workspaceModel) Init() tea.Cmd {
	return tea.Batch(m.loadInstances(), m.loadCatalog(), m.loadNamespaces())
}

// loadNamespaces lists the namespaces that hold AgentTemplates.
func (m *workspaceModel) loadNamespaces() tea.Cmd {
	return func() tea.Msg {
		if m.catalog == nil {
			return namespacesLoadedMsg{err: m.noCatalogErr()}
		}
		namespaces, err := m.catalog.Namespaces(m.ctx)
		return namespacesLoadedMsg{namespaces: namespaces, err: err}
	}
}

// loadCatalog reads the Harness and AgentTemplate names from Kubernetes.
func (m *workspaceModel) loadCatalog() tea.Cmd {
	return func() tea.Msg {
		if m.catalog == nil {
			return catalogLoadedMsg{err: m.noCatalogErr()}
		}

		harnesses, err := m.catalog.Harnesses(m.ctx, m.namespace)
		if err != nil {
			return catalogLoadedMsg{err: err}
		}
		templates, err := m.catalog.AgentTemplates(m.ctx, m.namespace)
		if err != nil {
			return catalogLoadedMsg{err: err}
		}
		return catalogLoadedMsg{harnesses: harnesses, templates: templates}
	}
}

// loadInstances walks every page, bounded so a large deployment cannot stall startup.
func (m *workspaceModel) loadInstances() tea.Cmd {
	return func() tea.Msg {
		if m.lister == nil {
			return instancesLoadedMsg{err: fmt.Errorf("no kagent client configured")}
		}

		var (
			instances []*apiv1alpha1.AgentInstance
			pageToken string
		)
		for range maxInstancePages {
			response, err := m.lister.ListAgentInstances(m.ctx, &apiv1alpha1.ListAgentInstancesRequest{
				Namespace: m.namespace,
				Page:      &apiv1alpha1.PageRequest{Limit: instancePageSize, PageToken: pageToken},
			})
			if err != nil {
				return instancesLoadedMsg{err: err}
			}
			instances = append(instances, response.GetAgentInstances()...)
			pageToken = response.GetPage().GetNextPageToken()
			if pageToken == "" {
				return instancesLoadedMsg{instances: instances}
			}
		}
		return instancesLoadedMsg{instances: instances, truncated: true}
	}
}

// loadHistory reads past tasks. The gateway orders them by random task UUID,
// so they are sorted here into a coherent transcript.
func (m *workspaceModel) loadHistory(agentInstance *apiv1alpha1.AgentInstance) tea.Cmd {
	id := agentInstance.GetId()
	return func() tea.Msg {
		a2aClient, err := m.client.A2A.ForAgentInstance(m.ctx, m.namespace, id)
		if err != nil {
			return instanceHistoryLoadedMsg{instanceID: id, err: err}
		}
		historyLength := historyMessageLimit
		response, err := a2aClient.ListTasks(m.ctx, &a2atype.ListTasksRequest{
			ContextID:        id,
			PageSize:         historyTaskLimit,
			HistoryLength:    &historyLength,
			IncludeArtifacts: true,
		})
		if err != nil {
			return instanceHistoryLoadedMsg{instanceID: id, err: err}
		}
		tasks := response.Tasks
		slices.SortStableFunc(tasks, compareTaskTime)
		return instanceHistoryLoadedMsg{instanceID: id, tasks: tasks}
	}
}

// compareTaskTime orders oldest first by status timestamp, the only time a task carries.
func compareTaskTime(a, b *a2atype.Task) int {
	if a == nil || b == nil || a.Status.Timestamp == nil || b.Status.Timestamp == nil {
		return 0
	}
	return a.Status.Timestamp.Compare(*b.Status.Timestamp)
}

func (m *workspaceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.resize()

	case instancesLoadedMsg:
		return m, m.applyInstances(msg)

	case catalogLoadedMsg:
		// Without a catalog the panels still work, so this only warns.
		if msg.err != nil {
			m.status = fmt.Sprintf("Listing only AgentTemplates that have instances: %v", msg.err)
			return m, nil
		}
		m.catalogHarnesses, m.catalogTemplates = msg.harnesses, msg.templates
		m.rebuildPanels()
		return m, m.resize()

	case namespacesLoadedMsg:
		m.applyNamespaces(msg)
		return m, m.resize()

	case instanceSelectedMsg:
		return m, m.selectInstance(msg.agentInstance)

	case instanceHistoryLoadedMsg:
		if msg.instanceID != m.current.GetId() || m.chat == nil {
			return m, nil // the user selected something else while this was in flight
		}
		if msg.err != nil {
			m.status = fmt.Sprintf("Failed to load history: %v", msg.err)
			return m, nil
		}
		for _, task := range msg.tasks {
			m.chat.AppendHistoryTask(task)
		}
		// Tasks are sorted oldest first, so the last is the most recent thing
		// this instance actually did.
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

	// Stream and timer messages go to the chat wherever focus is. Routing them
	// by focus would strand a reply as soon as the user looked at a panel.
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

// sidebarPanel is one stacked list panel, the height it is drawn at, and how
// its rows render.
type sidebarPanel struct {
	id     panelID
	list   *list.Model
	height int
	row    func(list.Item, int) string
}

// sidebarPanels is the single source of the sidebar's geometry: order, heights,
// and therefore every screen offset. The instance panel takes what is left.
func (m *workspaceModel) sidebarPanels() []sidebarPanel {
	namespaces := filterPanelHeight(m.namespaces)
	harnesses := filterPanelHeight(m.harnesses)
	templates := filterPanelHeight(m.templates)
	return []sidebarPanel{
		{panelNamespaces, &m.namespaces, namespaces, nameRow},
		{panelHarnesses, &m.harnesses, harnesses, nameRow},
		{panelTemplates, &m.templates, templates, nameRow},
		{panelInstances, &m.instances, max(m.bodyHeight()-namespaces-harnesses-templates, 3), instanceRow},
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
	return panelInstances, true
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

// applyInstances stores the fetched instances and rebuilds every panel.
func (m *workspaceModel) applyInstances(msg instancesLoadedMsg) tea.Cmd {
	if msg.err != nil {
		m.status = fmt.Sprintf("Failed to load AgentInstances: %v", msg.err)
		return nil
	}
	m.status = ""
	if msg.truncated {
		m.status = fmt.Sprintf("Showing the first %d AgentInstances; more pages are available.", len(msg.instances))
	}

	m.all = msg.instances
	slices.SortStableFunc(m.all, func(a, b *apiv1alpha1.AgentInstance) int {
		return b.GetCreatedAt().AsTime().Compare(a.GetCreatedAt().AsTime()) // newest first
	})
	m.rebuildPanels()

	if m.current == nil {
		return m.openSelectedInstance()
	}
	// A refresh may have deleted the open instance or changed its state, so
	// re-read it rather than leaving a stale chat and header behind.
	for _, agentInstance := range m.all {
		if agentInstance.GetId() == m.current.GetId() {
			if agentInstance.GetState() != m.current.GetState() {
				return m.selectInstance(agentInstance)
			}
			m.current = agentInstance
			m.renderDetails()
			return nil
		}
	}
	m.chat.stop()
	m.chat, m.current = nil, nil
	m.status = "The open AgentInstance no longer exists."
	return m.openSelectedInstance()
}

// rebuildPanels derives the cascade panels from the instances already fetched,
// then narrows the instance panel by the current selections.
func (m *workspaceModel) rebuildPanels() {
	m.harnesses.SetItems(countedNames(m.catalogHarnesses, m.all, func(i *apiv1alpha1.AgentInstance) string {
		return i.GetHarness().GetName()
	}))
	m.rebuildTemplates()
}

// rebuildTemplates also rebuilds the instances below it, since SetItems resets
// this panel's cursor and so clears whatever template was selected.
func (m *workspaceModel) rebuildTemplates() {
	m.templates.SetItems(countedNames(m.catalogTemplates, m.filterByHarness(), func(i *apiv1alpha1.AgentInstance) string {
		return i.GetAgentTemplate().GetName()
	}))
	m.template = ""
	m.rebuildInstances()
}

func (m *workspaceModel) rebuildInstances() {
	visible := m.visibleInstances()
	items := make([]list.Item, 0, len(visible))
	for _, agentInstance := range visible {
		items = append(items, instanceItem{AgentInstance: agentInstance})
	}
	m.instances.SetItems(items)
}

// applyNamespaces fills the namespace panel, always including the current
// namespace so the user can see where they are even if the list failed.
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
		items = append(items, nameItem{name: namespace.Name, count: namespace.Templates})
	}
	m.namespaces.SetItems(items)
	for i, namespace := range namespaces {
		if namespace.Name == m.namespace {
			m.namespaces.Select(i)
		}
	}
}

// syncCascade applies the cascade panels' cursors. Moving the cursor filters
// immediately, so there is nothing to confirm with enter. A namespace change
// refetches, because every request below it is namespace-scoped.
func (m *workspaceModel) syncCascade() tea.Cmd {
	if namespace := selectedNamespace(m.namespaces); namespace != "" && namespace != m.namespace {
		m.namespace = namespace
		m.harness, m.template = "", ""
		m.all, m.catalogHarnesses, m.catalogTemplates = nil, nil, nil
		m.current, m.chat = nil, nil
		m.rebuildPanels()
		return tea.Batch(m.loadInstances(), m.loadCatalog())
	}
	if harness := selectedName(m.harnesses); harness != m.harness {
		m.harness = harness
		m.rebuildTemplates() // a different harness invalidates the template below
		return nil
	}
	if template := selectedName(m.templates); template != m.template {
		m.template = template
		m.rebuildInstances()
	}
	return nil
}

// selectedNamespace reads the namespace panel's cursor. Unlike the filter
// panels it has no "(all)" row, since every request is namespace-scoped.
func selectedNamespace(panel list.Model) string {
	item, ok := panel.SelectedItem().(nameItem)
	if !ok {
		return ""
	}
	return item.name
}

// countedNames lists every catalog name with its instance count, after an "(all)"
// row. Unused names still get a row; so do names seen only on instances.
func countedNames(catalogNames []string, instances []*apiv1alpha1.AgentInstance, name func(*apiv1alpha1.AgentInstance) string) []list.Item {
	counts := map[string]int{}
	for _, value := range catalogNames {
		if value != "" {
			counts[value] = 0
		}
	}
	for _, agentInstance := range instances {
		if value := name(agentInstance); value != "" {
			counts[value]++
		}
	}

	order := make([]string, 0, len(counts))
	for value := range counts {
		order = append(order, value)
	}
	slices.Sort(order)

	items := make([]list.Item, 0, len(order)+1)
	items = append(items, nameItem{name: allNames, count: len(instances)})
	for _, value := range order {
		items = append(items, nameItem{name: value, count: counts[value]})
	}
	return items
}

func (m *workspaceModel) filterByHarness() []*apiv1alpha1.AgentInstance {
	if m.harness == "" {
		return m.all
	}
	var kept []*apiv1alpha1.AgentInstance
	for _, agentInstance := range m.all {
		if agentInstance.GetHarness().GetName() == m.harness {
			kept = append(kept, agentInstance)
		}
	}
	return kept
}

// visibleInstances applies both cascade filters.
func (m *workspaceModel) visibleInstances() []*apiv1alpha1.AgentInstance {
	var kept []*apiv1alpha1.AgentInstance
	for _, agentInstance := range m.filterByHarness() {
		if m.template == "" || agentInstance.GetAgentTemplate().GetName() == m.template {
			kept = append(kept, agentInstance)
		}
	}
	return kept
}

// openSelectedInstance opens whatever the instance panel currently highlights.
func (m *workspaceModel) openSelectedInstance() tea.Cmd {
	item, ok := m.instances.SelectedItem().(instanceItem)
	if !ok {
		return nil
	}
	selected := item.AgentInstance
	return func() tea.Msg { return instanceSelectedMsg{agentInstance: selected} }
}

// selectInstance opens a chat for an instance. A non-READY instance is not
// dialed at all: the gateway rejects every call for one, history included.
func (m *workspaceModel) selectInstance(agentInstance *apiv1alpha1.AgentInstance) tea.Cmd {
	if agentInstance == nil {
		return nil
	}
	m.chat.stop() // otherwise its stream delivers into the next instance's chat
	m.current = agentInstance
	m.renderDetails()

	if !instance.Ready(agentInstance) {
		m.chat = nil
		m.status = fmt.Sprintf("AgentInstance is %s and cannot accept messages.", instance.StateLabel(agentInstance.GetState()))
		return nil
	}

	m.status = ""
	a2aClient, err := m.client.A2A.ForAgentInstance(m.ctx, m.namespace, agentInstance.GetId())
	if err != nil {
		m.chat = nil
		m.status = fmt.Sprintf("Failed to connect to AgentInstance: %v", err)
		return nil
	}

	send := func(ctx context.Context, req *a2atype.SendMessageRequest) <-chan clia2a.StreamResult {
		return clia2a.StreamToChannel(ctx, a2aClient, req)
	}
	m.chat = newChatModel(m.ctx, agentInstance.GetAgentTemplate().GetName(), agentInstance.GetId(), send, m.verbose)
	m.chat.setHeaderMeta(stateBadge(agentInstance.GetState()), agentInstance.GetUpdatedAt().AsTime())
	// Bubble Tea calls Init only on the root model, so start the chat's here.
	return tea.Batch(m.chat.Init(), m.resize(), m.loadHistory(agentInstance))
}

// handleKey reports whether it consumed the key; the rest fall through to the focused panel.
func (m *workspaceModel) handleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	// An open filter input owns every key, including the panel digits.
	if m.filtering() {
		return nil, false
	}

	switch msg.String() {
	case "ctrl+c":
		// Cancel the in-flight stream before the program tears down, rather
		// than leaving the request to be dropped by process exit.
		m.chat.stop()
		return tea.Quit, true
	case "ctrl+r":
		return m.loadInstances(), true
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

// activateFocused narrows the cascade, or opens a chat from the instance panel.
func (m *workspaceModel) activateFocused() (tea.Cmd, bool) {
	switch m.focus {
	case panelNamespaces, panelHarnesses, panelTemplates:
		// The cursor already applied the filter, so enter just drills down.
		m.focus = m.focus.next()
		return m.resize(), true
	case panelInstances:
		return m.openSelectedInstance(), true
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
	case panelHarnesses:
		m.harnesses, cmd = m.harnesses.Update(msg)
		cmd = tea.Batch(cmd, m.syncCascade())
	case panelTemplates:
		m.templates, cmd = m.templates.Update(msg)
		cmd = tea.Batch(cmd, m.syncCascade())
	case panelInstances:
		m.instances, cmd = m.instances.Update(msg)
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

// filterPanelHeight sizes a cascade panel to its rows, within bounds. The extra
// line is bubbles' own spacing; without it the last row is silently clipped.
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

// renderDetails fills the right pane with the selected instance's identity.
func (m *workspaceModel) renderDetails() {
	if m.current == nil {
		m.details = ""
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ID\n%s\n\n", m.current.GetId())
	fmt.Fprintf(&b, "AgentTemplate\n%s\n\n", m.current.GetAgentTemplate().GetName())
	fmt.Fprintf(&b, "Harness\n%s\n\n", m.current.GetHarness().GetName())
	fmt.Fprintf(&b, "State\n%s\n\n", instance.StateLabel(m.current.GetState()))
	fmt.Fprintf(&b, "Namespace\n%s\n", m.current.GetNamespace())
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
		return "No AgentInstances.\n\nCreate one with:\nkagent create agent-instance --harness H --agent-template T"
	}
	if m.current != nil {
		return fmt.Sprintf("AgentInstance is %s.\n\nIt cannot accept messages right now.\nPress ctrl+r to refresh, or pick another in panel [3].",
			instance.StateLabel(m.current.GetState()))
	}
	return "Select an AgentInstance in panel [3] to start chatting."
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

// noCatalogErr explains why there is no catalog, preserving the kubeconfig
// failure rather than reporting a generic absence.
func (m *workspaceModel) noCatalogErr() error {
	if m.catalogErr != nil {
		return m.catalogErr
	}
	return errors.New("no Kubernetes client configured")
}
