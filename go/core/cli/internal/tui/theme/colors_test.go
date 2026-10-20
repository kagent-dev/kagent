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
