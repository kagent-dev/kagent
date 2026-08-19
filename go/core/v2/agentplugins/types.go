package agentplugins

const ConfigEnv = "KAGENT_AGENT_PLUGINS_JSON"

// Config is the immutable plugin and standalone-skill input embedded in a
// prepared runtime revision.
type Config struct {
	Skills  []Skill  `json:"skills,omitempty"`
	Plugins []Plugin `json:"plugins,omitempty"`
}

type Skill struct {
	Name   string `json:"name"`
	Source Source `json:"source"`
}

type Plugin struct {
	Source Source   `json:"source"`
	Skills []string `json:"skills,omitempty"`
}

type Source struct {
	OCI  string `json:"oci,omitempty"`
	Git  *Git   `json:"git,omitempty"`
	S3   *S3    `json:"s3,omitempty"`
	Path string `json:"path,omitempty"`
}

type Git struct {
	URL    string `json:"url"`
	Commit string `json:"commit"`
}

type S3 struct {
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	VersionID string `json:"versionId"`
	Region    string `json:"region,omitempty"`
}
