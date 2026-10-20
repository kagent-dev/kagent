package theme

import (
	"math"
	"strconv"
	"testing"
)

// luminance is a hex colour's relative luminance, as WCAG defines it.
func luminance(hex string) float64 {
	var channels [3]float64
	for i := range channels {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c := float64(v) / 255
		if c <= 0.03928 {
			channels[i] = c / 12.92
		} else {
			channels[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}

func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

// The focused border, which the input's divider shares, reads clearly in both
// themes, and the unfocused border stays visible yet plainly dimmer.
func TestBorderContrast(t *testing.T) {
	if got := contrast(ColorPrimary.Dark, "#000000"); got < 4.5 {
		t.Errorf("dark focused border is %.2f:1 on black", got)
	}
	if got := contrast(ColorPrimary.Light, "#FFFFFF"); got < 4.5 {
		t.Errorf("light focused border is %.2f:1 on white", got)
	}
	if got := contrast(ColorBorder.Dark, "#000000"); got < 3 {
		t.Errorf("dark unfocused border is %.2f:1 on black", got)
	}
	if got := contrast(ColorBorder.Dark, ColorPrimary.Dark); got < 2 {
		t.Errorf("dark unfocused border is only %.2f:1 against the focused one", got)
	}
}

// Chat text reads clearly on common dark backgrounds and on white; faint text stays at least 4.5:1.
func TestTextContrast(t *testing.T) {
	darks := []string{"#000000", "#0D1117", "#1E1E2E"}
	check := func(name, color string, backgrounds []string, least float64) {
		for _, background := range backgrounds {
			if got := contrast(color, background); got < least {
				t.Errorf("%s %s is %.2f:1 on %s, want %.1f", name, color, got, background, least)
			}
		}
	}
	check("text", ColorText.Dark, darks, 10)
	check("text", ColorText.Light, []string{"#FFFFFF"}, 10)
	check("bold", ColorStrong.Dark, darks, 15)
	check("bold", ColorStrong.Light, []string{"#FFFFFF"}, 15)
	for name, c := range map[string]struct{ dark, light string }{
		"link": {ColorLink.Dark, ColorLink.Light}, "heading": {ColorAccent.Dark, ColorAccent.Light},
		"code": {ColorAttention.Dark, ColorAttention.Light}, "faint": {ColorFaint.Dark, ColorFaint.Light},
		"muted": {ColorMuted.Dark, ColorMuted.Light}, "code block": {ColorCodeBlock.Dark, ColorCodeBlock.Light},
	} {
		check(name, c.dark, darks, 4.5)
		check(name, c.light, []string{"#FFFFFF"}, 4.5)
	}
	if luminance(ColorStrong.Dark) <= luminance(ColorText.Dark) || luminance(ColorStrong.Light) >= luminance(ColorText.Light) {
		t.Error("bold must stand out from body text in both themes")
	}
}
