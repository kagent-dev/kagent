// Package agentplugin defines the runtime-neutral configuration for immutable
// standalone skills and Agent Plugin packages.
package agentplugin

// Resources describes the skill resources a Harness must materialize before
// starting an agent.
type Resources struct {
	Skills  []Skill  `json:"skills,omitempty"`
	Plugins []Bundle `json:"plugins,omitempty"`
}

// Skill identifies one independently sourced skill.
type Skill struct {
	Name   string `json:"name"`
	Source Source `json:"source"`
}

// Bundle selects named skills from one immutable Agent Plugin package.
type Bundle struct {
	Source Source   `json:"source"`
	Skills []string `json:"skills,omitempty"`
}

// Source selects one immutable artifact and an optional directory within it.
type Source struct {
	OCI  string     `json:"oci,omitempty"`
	Git  *GitSource `json:"git,omitempty"`
	S3   *S3Source  `json:"s3,omitempty"`
	Path string     `json:"path,omitempty"`
}

// GitSource identifies one immutable Git commit.
type GitSource struct {
	URL    string `json:"url"`
	Commit string `json:"commit"`
}

// S3Source identifies one immutable S3 object version.
type S3Source struct {
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	VersionID string `json:"versionId"`
	Region    string `json:"region,omitempty"`
}
