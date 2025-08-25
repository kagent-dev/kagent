package profiles

import _ "embed"

//go:embed demo.yaml
var DemoProfile string

//go:embed minimal.yaml
var MinimalProfile string

const (
	ProfileDemo    = "demo"
	ProfileMinimal = "minimal"
)

var Profiles = []string{ProfileMinimal, ProfileDemo}

func GetProfile(profile string) string {
	switch profile {
	case ProfileDemo:
		return DemoProfile
	case ProfileMinimal:
		return MinimalProfile
	default:
		return MinimalProfile
	}
}
