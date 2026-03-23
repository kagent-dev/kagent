package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/kagent-dev/kagent/go/core/cli/internal/tui/theme"
)

func RenderUsageGauge(rate float64, width int) string {
	if rate < 0 {
		rate = 0
	} else if rate > 1 {
		rate = 1
	}

	filledWidth := int(rate * float64(width))
	emptyWidth := width - filledWidth

	barColor := theme.ColorPrimary

	filled := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("■", filledWidth))
	empty := lipgloss.NewStyle().Foreground(theme.ColorBorder).Render(strings.Repeat("■", emptyWidth))

	return fmt.Sprintf("[%s%s] %3.0f%%", filled, empty, rate*100)
}
