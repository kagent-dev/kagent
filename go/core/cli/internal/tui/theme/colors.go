package theme

import "github.com/charmbracelet/lipgloss"

var (
	ColorPrimary = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"} // primary/ring, the focused border
	ColorMuted   = lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#94A3B8"} // muted-foreground
	ColorBorder  = lipgloss.AdaptiveColor{Light: "#E5E7EB", Dark: "#525C6B"} // border, unfocused

	// ColorError is red-700 on light and red-400 on dark, both over 6:1, so error text stays readable.
	ColorError    = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	ColorSelected = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#6D28D9"} // primary/ring
	ColorReady    = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#10B981"} // emerald
	// ColorAttention is amber, for code and what waits on the user: 5.0:1 on white, 12.6:1 on black.
	ColorAttention = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	// ColorAccent is primary at text contrast: violet-700 on white, violet-400 on black, both over 7:1.
	ColorAccent = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#A78BFA"}
	// ColorText is agent text: 16:1 on #0d1117 and 15:1 on white.
	ColorText = lipgloss.AdaptiveColor{Light: "#1F2328", Dark: "#E6EDF3"}
	// ColorStrong is bold text, a step past ColorText so it stands out.
	ColorStrong = lipgloss.AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}
	// ColorLink is links and file paths: 5.1:1 on white, 9:1 on #0d1117.
	ColorLink = lipgloss.AdaptiveColor{Light: "#0969DA", Dark: "#79C0FF"}
	// ColorCodeBlock is code block text, readable but set apart from prose.
	ColorCodeBlock = lipgloss.AdaptiveColor{Light: "#24292F", Dark: "#C9D1D9"}
	// ColorFaint recedes behind body text but stays over 4.5:1 on white, #0d1117 and #1e1e2e.
	ColorFaint = lipgloss.AdaptiveColor{Light: "#6E7681", Dark: "#8B949E"}
)

func HeadingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true)
}

func UserStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(ColorAccent).Bold(true) }

func ErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorError).Bold(true)
}

func SeparatorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorBorder)
}

func StatusStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorMuted)
}

// LinkStyle marks links and file paths.
func LinkStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorLink).Underline(true)
}

// ReadyStyle marks success: a finished tool call and added lines.
func ReadyStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorReady)
}

// AttentionStyle marks what waits on the user, or a call that was denied.
func AttentionStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorAttention).Bold(true)
}

// FaintStyle is for what sits behind the conversation, such as collapsed tool output.
func FaintStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorFaint)
}

func DimStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorMuted)
}
