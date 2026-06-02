// Package skillsinit defines the contract between the kagent controller and
// the skills-init container binary. The controller renders a Config to JSON,
// projects it via a ConfigMap, and mounts it at /etc/kagent/skills-init. The
// binary deserializes it and performs the fetch operations.
//
// User-controlled values flow through structured JSON and then into argv-style
// process calls (never a shell), so shell metacharacters in user fields are
// inert and cannot trigger command execution.
package skillsinit

const (
	// ConfigMountPath is where the ConfigMap is mounted inside the container.
	ConfigMountPath = "/etc/kagent/skills-init"
	// ConfigFileName is the file the binary reads inside ConfigMountPath.
	ConfigFileName = "config.json"
	// ConfigMapKey is the key the controller writes inside the ConfigMap.
	ConfigMapKey = "config.json"

	// SkillsDir is the shared volume both containers see; the binary writes
	// fetched skill contents here.
	SkillsDir = "/skills"
	// AuthMountPath is where the optional git auth secret is mounted.
	AuthMountPath = "/git-auth"
	// DockerSecretsDir is where dockerconfigjson secrets are mounted, one
	// per directory keyed by secret name.
	DockerSecretsDir = "/docker-secrets"
)

// Config is the full input the binary expects. Field names are stable and any
// change requires bumping the controller and the binary in lockstep.
type Config struct {
	// AuthMountPath is non-empty when a gitAuthSecretRef was configured.
	// When set the binary expects a token or ssh-privatekey at this path.
	AuthMountPath string `json:"authMountPath,omitempty"`

	// GitRefs is the list of git repositories to clone.
	GitRefs []GitRef `json:"gitRefs,omitempty"`

	// OCIRefs is the list of OCI images to pull and extract.
	OCIRefs []OCIRef `json:"ociRefs,omitempty"`

	// InsecureOCI allows pulling from registries with untrusted certs.
	InsecureOCI bool `json:"insecureOci,omitempty"`

	// SSHHosts are entries that should be pre-populated in ~/.ssh/known_hosts
	// via ssh-keyscan before any git clone runs.
	SSHHosts []SSHHost `json:"sshHosts,omitempty"`

	// ImagePullSecrets is the list of dockerconfigjson secret names mounted
	// under DockerSecretsDir. The binary merges them into a single config.json
	// that go-containerregistry consults during OCI pulls.
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`
}

// GitRef describes a single git clone operation.
type GitRef struct {
	URL string `json:"url"`
	// Ref is a branch name, tag, or commit SHA.
	Ref  string `json:"ref"`
	Dest string `json:"dest"`
	// Full requests a full (non-shallow) clone followed by `git checkout`.
	// Set when Ref is a commit SHA, since shallow `--branch` does not accept
	// SHAs. The default (false) is a depth-1 branch/tag clone.
	Full bool `json:"full,omitempty"`
	// SubPath, if set, names a subdirectory inside the clone that becomes
	// the final skill root.
	SubPath string `json:"subPath,omitempty"`
}

// OCIRef describes a single OCI image to pull and extract.
type OCIRef struct {
	Image string `json:"image"`
	Dest  string `json:"dest"`
}

// SSHHost is a known_hosts entry to seed with ssh-keyscan.
type SSHHost struct {
	Host string `json:"host"`
	Port string `json:"port,omitempty"`
}
