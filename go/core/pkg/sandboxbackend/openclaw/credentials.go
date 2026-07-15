package openclaw

import (
	"fmt"
	"strings"
)

func sandboxChannelEnvSuffix(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(name)) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return "CH"
	}
	return s
}

func channelSecretEnvVar(channelName, tokenRole string) string {
	return fmt.Sprintf("KAGENT_SB_CH_%s_%s", sandboxChannelEnvSuffix(channelName), tokenRole)
}
