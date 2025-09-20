package theme

import "github.com/charmbracelet/lipgloss"

var (
	ColorPrimary = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#6D28D9"} // primary/ring
	ColorMuted   = lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#94A3B8"} // muted-foreground
	ColorBorder  = lipgloss.AdaptiveColor{Light: "#E5E7EB", Dark: "#1F2937"} // border

	ColorError    = lipgloss.AdaptiveColor{Light: "#991B1B", Dark: "#7F1D1D"} // destructive
	ColorSelected = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#6D28D9"} // primary/ring
)

func HeadingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true)
}

func UserStyle() lipgloss.Style  { return lipgloss.NewStyle().Bold(true) }
func AgentStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true) }

func ErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorError).Bold(true)
}

func SeparatorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorBorder)
}

func StatusStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorMuted)
}

func ToolStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorMuted)
}
