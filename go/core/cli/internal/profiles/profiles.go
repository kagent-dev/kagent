package profiles

import _ "embed"

//go:embed demo.yaml
var DemoProfileYaml string

//go:embed minimal.yaml
var MinimalProfileYaml string

const (
	ProfileDemo    = "demo"
	ProfileMinimal = "minimal"
)

var Profiles = []string{ProfileMinimal, ProfileDemo}

func GetProfileYaml(profile string) string {
	switch profile {
	case ProfileDemo:
		return DemoProfileYaml
	case ProfileMinimal:
		return MinimalProfileYaml
	default:
		return DemoProfileYaml
	}
}
