package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/instance"
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/theme"
)

// panelID identifies a focusable region; the value is the key that focuses it.
type panelID int

const (
	panelChat       panelID = 0
	panelNamespaces panelID = 1
	panelHarnesses  panelID = 2
	panelTemplates  panelID = 3
	panelInstances  panelID = 4
	lastListPanel           = panelInstances
)

func (p panelID) title() string {
	switch p {
	case panelNamespaces:
		return "Namespaces"
	case panelHarnesses:
		return "Harnesses"
	case panelTemplates:
		return "AgentTemplates"
	case panelInstances:
		return "AgentInstances"
	default:
		return "Chat"
	}
}

// next cycles focus across every panel, wrapping back to the chat.
func (p panelID) next() panelID {
	if p >= lastListPanel {
		return panelChat
	}
	return p + 1
}

// Panel geometry: 2 columns of border plus 2 of padding, 3 rows of chrome.
// Hand these to whatever fills a panel, or its content wraps.
const panelChromeHeight = 3 // both borders plus the title line

func panelInnerWidth(total int) int  { return max(total-4, 8) }
func panelInnerHeight(total int) int { return max(total-panelChromeHeight, 1) }

// panelBox draws a bordered panel titled with its number, highlighted when focused.
func panelBox(id panelID, content string, width, height int, focused bool) string {
	border := theme.ColorBorder
	titleStyle := theme.DimStyle()
	if focused {
		border = theme.ColorPrimary
		titleStyle = lipgloss.NewStyle().Foreground(theme.ColorPrimary).Bold(true)
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(fmt.Sprintf("[%d] %s", id, id.title())),
		lipgloss.NewStyle().
			Width(panelInnerWidth(width)).
			Height(panelInnerHeight(height)).
			Render(trimBlankLines(content)),
	)
	return lipgloss.NewStyle().
		Width(width-2). // lipgloss adds the border outside the styled width
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Render(body)
}

// newPanelList strips the list's own chrome; panelBox draws the title and border.
func newPanelList(delegate list.ItemDelegate) list.Model {
	panelList := list.New(nil, delegate, 0, 0)
	panelList.SetShowTitle(false)
	panelList.SetShowHelp(false)
	panelList.SetShowStatusBar(false)
	panelList.SetShowPagination(false)
	panelList.SetFilteringEnabled(true)
	return panelList
}

// nameItem is a harness or template row, with how many AgentInstances it holds.
type nameItem struct {
	name  string
	count int
}

func (i nameItem) FilterValue() string { return i.name }

// instanceItem adapts an AgentInstance to a panel row.
type instanceItem struct {
	*apiv1alpha1.AgentInstance
}

// FilterValue lets `/` match on template, ID prefix, or state.
func (i instanceItem) FilterValue() string {
	return strings.Join([]string{
		i.GetAgentTemplate().GetName(),
		i.GetId(),
		instance.StateLabel(i.GetState()),
	}, " ")
}

// stateGlyph uses shape, not colour alone, so it survives a monochrome terminal.
func stateGlyph(state apiv1alpha1.AgentInstanceState) (string, lipgloss.AdaptiveColor) {
	switch state {
	case apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY:
		return "●", theme.ColorReady
	case apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED:
		return "○", theme.ColorMuted
	case apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_FAILED:
		return "✗", theme.ColorError
	case apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING:
		return "◐", theme.ColorMuted
	default:
		return "·", theme.ColorMuted
	}
}

// rowDelegate renders one compact line per item, with a cursor on the selection.
type rowDelegate struct {
	width int
	row   func(item list.Item, width int) string
}

func (d rowDelegate) Height() int                         { return 1 }
func (d rowDelegate) Spacing() int                        { return 0 }
func (d rowDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d rowDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	cursor := "  "
	style := lipgloss.NewStyle()
	if index == m.Index() {
		cursor = "> "
		style = style.Foreground(theme.ColorPrimary).Bold(true)
	}
	fmt.Fprint(w, style.Render(cursor+d.row(item, max(d.width-2, 8))))
}

// nameRow renders a name with its count right-aligned.
func nameRow(item list.Item, width int) string {
	named, ok := item.(nameItem)
	if !ok {
		return ""
	}
	count := fmt.Sprintf("%d", named.count)
	nameWidth := max(width-len(count)-1, 1)
	return pad(truncate(named.name, nameWidth), nameWidth) + " " + theme.DimStyle().Render(count)
}

// trimBlankLines drops blank edges so list spacing does not push rows off the title.
func trimBlankLines(content string) string {
	lines := strings.Split(content, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// instanceRow renders an AgentInstance row: state glyph, template, short ID.
func instanceRow(item list.Item, width int) string {
	row, ok := item.(instanceItem)
	if !ok {
		return ""
	}
	glyph, colour := stateGlyph(row.GetState())
	shortID := instance.ShortID(row.GetId())
	name := truncate(row.GetAgentTemplate().GetName(), max(width-len(shortID)-4, 1))
	return fmt.Sprintf("%s %s %s",
		lipgloss.NewStyle().Foreground(colour).Render(glyph),
		pad(name, max(width-len(shortID)-4, 1)),
		theme.DimStyle().Render(shortID),
	)
}

// truncate shortens text to width columns, marking what it cut.
func truncate(text string, width int) string {
	if len(text) <= width {
		return text
	}
	if width <= 1 {
		return text[:max(width, 0)]
	}
	return text[:width-1] + "…"
}

// pad right-fills text to width columns so later columns line up.
func pad(text string, width int) string {
	if len(text) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-len(text))
}
