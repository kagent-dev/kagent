package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/kagent-dev/kagent/go/core/cli/internal/experimental_tui/domain"
)

var (
	colIDWidth     = 8
	colNameWidth   = 20
	colStatusWidth = 12
	colGaugeWidth  = 20

	headerStyle = lipgloss.NewStyle().
			Bold(true)

	selectedRowStyle = lipgloss.NewStyle().
				Bold(true)

	normalRowStyle = lipgloss.NewStyle()
)

type tableColumnWidth struct {
	ID     int
	Name   int
	Status int
	Gauge  int
}

func RenderTable(agents []domain.Agent, cursor int) string {
	columnWidth := tableColumnWidth{
		ID:     colIDWidth,
		Name:   colNameWidth,
		Status: colStatusWidth,
		Gauge:  colGaugeWidth,
	}

	for _, agent := range agents {
		if w := lipgloss.Width(agent.ID); w > columnWidth.ID {
			columnWidth.ID = w
		}
		if w := lipgloss.Width(agent.Name); w > columnWidth.Name {
			columnWidth.Name = w
		}
		statusBadge := agent.Status
		if w := lipgloss.Width(string(statusBadge)); w > columnWidth.Status {
			columnWidth.Status = w
		}
		gauge := RenderUsageGauge(agent.ContextUsageRate(), colGaugeWidth)
		if w := lipgloss.Width(gauge); w > columnWidth.Gauge {
			columnWidth.Gauge = w
		}
	}

	header := fmt.Sprintf(" %-*s %-*s %-*s %-*s ",
		columnWidth.ID, "ID",
		columnWidth.Name, "NAME",
		columnWidth.Status, "STATUS",
		columnWidth.Gauge, "CONTEXT USAGE",
	)

	var b strings.Builder
	b.WriteString(headerStyle.Render(header) + "\n")
	for i, agent := range agents {
		b.WriteString(RenderAgentRow(agent, i == cursor, columnWidth) + "\n")
	}
	return b.String()
}

func RenderAgentRow(agent domain.Agent, selected bool, columnWidth tableColumnWidth) string {
	statusBadge := strings.ToUpper(string(agent.Status))

	gauge := RenderUsageGauge(agent.ContextUsageRate(), columnWidth.Gauge)

	visibleWidth := lipgloss.Width(statusBadge)
	padding := columnWidth.Status - visibleWidth
	if padding < 0 {
		padding = 0
	}
	paddedStatus := statusBadge + strings.Repeat(" ", padding)

	rowStr := fmt.Sprintf(" %-*s %-*s %-*s %-*s ",
		columnWidth.ID, agent.ID,
		columnWidth.Name, agent.Name,
		columnWidth.Status, paddedStatus,
		columnWidth.Gauge, gauge,
	)
	if selected {
		return selectedRowStyle.Render(rowStr)
	}

	return normalRowStyle.Render(rowStr)
}
