package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/kagent-dev/kagent/go/core/cli/internal/experimental_tui/domain"
)

var baseStatusStyle = lipgloss.NewStyle().
	Bold(true).
	Padding(0, 1)

func RenderStatusBadge(status domain.AgentStatus) string {
	text := strings.ToUpper(string(status))

	return text
}
