package profiles

import _ "embed"

//go:embed minimal.yaml
var MinimalProfileYaml string

const ProfileMinimal = "minimal"

var Profiles = []string{ProfileMinimal}
