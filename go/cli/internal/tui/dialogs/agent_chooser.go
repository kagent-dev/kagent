package dialogs

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kagent-dev/kagent/go/cli/internal/tui/theme"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

type AgentItem struct{ api.AgentResponse }

func (i AgentItem) Title() string       { return i.Agent.Name }
func (i AgentItem) Namespace() string   { return i.Agent.Namespace }
func (i AgentItem) Description() string { return i.Agent.Spec.Description }
func (i AgentItem) FilterValue() string { return i.ID }

type AgentChooser struct {
	id      string
	items   []*AgentItem
	rows    []table.Row
	columns []table.Column
	table   table.Model
	onOK    func(item list.Item) tea.Cmd
}

func NewAgentChooser(items []*AgentItem, onOK func(item list.Item) tea.Cmd) *AgentChooser {
	ac := &AgentChooser{
		id:    "agent_chooser",
		items: items,
		onOK:  onOK,
	}
	ac.rows = ac.buildRows(items)
	ac.columns = ac.buildColumns(80)

	t := table.New(
		table.WithColumns(ac.columns),
		table.WithRows(ac.rows),
		table.WithFocused(true),
		table.WithHeight(8),
	)
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder).
		BorderBottom(true).
		Bold(false)
	styles.Selected = styles.Selected.
		Foreground(theme.ColorPrimary).
		Background(theme.ColorSelected).
		Bold(false)
	t.SetStyles(styles)
	ac.table = t
	return ac
}

func (a *AgentChooser) buildColumns(innerWidth int) []table.Column {
	// Agent should be ~ 20% of the width
	agentW := innerWidth * 2 / 10
	// Namespace should be ~ 5% of the width (with a small minimum)
	namespaceW := innerWidth * 5 / 100
	if namespaceW < 6 {
		namespaceW = 6
	}
	descW := innerWidth - agentW - namespaceW

	columns := []table.Column{
		{Title: "Agent", Width: agentW},
		{Title: "Namespace", Width: namespaceW},
		{Title: "Description", Width: descW},
	}
	return columns
}

func (a *AgentChooser) buildRows(items []*AgentItem) []table.Row {
	rows := make([]table.Row, 0, len(items))
	for _, it := range items {
		title := it.Title()
		namespace := it.Namespace()
		desc := it.Description()
		rows = append(rows, table.Row{title, namespace, desc})
	}
	return rows
}

func (a *AgentChooser) Init() tea.Cmd { return nil }

func (a *AgentChooser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		w := m.Width
		h := m.Height
		// adjust column widths to fit almost full-screen
		// min width is 80, we subtract 4 for padding/border
		innerWidth := max(80, w-4)
		a.table.SetColumns(a.buildColumns(innerWidth))
		a.table.SetHeight(max(10, h-4))
		return a, nil
	case tea.KeyMsg:
		switch m.String() {
		case "enter":
			if a.onOK != nil {
				idx := a.selectedIndex()
				if idx >= 0 && idx < len(a.items) {
					return a, tea.Batch(a.onOK(a.items[idx]), func() tea.Msg { return CloseMsg{} })
				}
			}
		case "esc":
			return a, func() tea.Msg { return CloseMsg{} }
		}
	}
	var cmd tea.Cmd
	a.table, cmd = a.table.Update(msg)
	return a, cmd
}

func (a *AgentChooser) selectedIndex() int {
	row := a.table.SelectedRow()
	if len(row) == 0 {
		return -1
	}
	// find by matching title and description
	for i, r := range a.rows {
		if len(r) == len(row) {
			match := true
			for j := range r {
				if r[j] != row[j] {
					match = false
					break
				}
			}
			if match {
				return i
			}
		}
	}
	return -1
}

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(theme.ColorBorder).
	Padding(2, 2)

func (a *AgentChooser) View() string {
	return baseStyle.Render(a.table.View())
}

func (a *AgentChooser) ID() string       { return a.id }
func (a *AgentChooser) Fullscreen() bool { return true }

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}
