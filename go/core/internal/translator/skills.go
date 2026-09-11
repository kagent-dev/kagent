package translator

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
)

// ArtifactCredentialEnvPrefix starts the environment variables that name the
// Secret keys artifact sources authenticate with. The suffix identifies the
// Secret key, so sources sharing a credential share one variable. The actor
// receives the variable with an inert placeholder: the egress gateway injects
// the credential into the request to the source's host.
const ArtifactCredentialEnvPrefix = "KAGENT_ARTIFACT_CREDENTIAL_"

// CompiledSkillResources is the harness-neutral result of compiling skills and
// plugins: the resources to materialize, the hosts they are fetched from, and
// the Secret-backed environment the credentials need. Credential values stay
// out of Resources; only the variable names are serialized.
type CompiledSkillResources struct {
	Resources   agentplugin.Resources
	Egress      []string
	Environment []corev1.EnvVar
}

// CompileSkillResources translates portable AgentTemplate skill selections
// into the runtime-neutral resource contract shared by Harness adapters.
func CompileSkillResources(template *v1alpha3.AgentTemplate) (CompiledSkillResources, error) {
	compiled := CompiledSkillResources{Resources: agentplugin.Resources{
		Skills:  make([]agentplugin.Skill, 0, len(template.Spec.Skills)),
		Plugins: make([]agentplugin.Bundle, 0, len(template.Spec.Plugins)),
	}}
	selected := make(map[string]struct{})
	credentials := map[string]struct{}{}
	compile := func(artifact v1alpha3.ArtifactSource) agentplugin.Source {
		source, credential := compileArtifactSource(template.Namespace, artifact)
		compiled.Egress = appendArtifactSourceDestination(compiled.Egress, source)
		if credential != nil {
			if _, exists := credentials[credential.Name]; !exists {
				credentials[credential.Name] = struct{}{}
				compiled.Environment = append(compiled.Environment, *credential)
			}
		}
		return source
	}
	for _, skill := range template.Spec.Skills {
		if _, exists := selected[skill.Name]; exists {
			return CompiledSkillResources{}, NewValidationError("duplicate skill name %q", skill.Name)
		}
		selected[skill.Name] = struct{}{}
		compiled.Resources.Skills = append(compiled.Resources.Skills, agentplugin.Skill{Name: skill.Name, Source: compile(skill.Source)})
	}
	for _, plugin := range template.Spec.Plugins {
		for _, name := range plugin.Skills {
			if _, exists := selected[name]; exists {
				return CompiledSkillResources{}, NewValidationError("duplicate skill name %q", name)
			}
			selected[name] = struct{}{}
		}
		compiled.Resources.Plugins = append(compiled.Resources.Plugins, agentplugin.Bundle{
			Source: compile(plugin.Source),
			Skills: append([]string(nil), plugin.Skills...),
		})
	}
	return compiled, nil
}

// compileArtifactSource returns the runtime source and, for a git source with
// a credentialRef, the Secret-backed environment variable that records the
// credential in the revision. The variable name derives from the Secret
// identity so the same credential compiles to the same variable in every
// revision.
func compileArtifactSource(namespace string, source v1alpha3.ArtifactSource) (agentplugin.Source, *corev1.EnvVar) {
	result := agentplugin.Source{OCI: source.OCI, Path: source.Path}
	var credential *corev1.EnvVar
	if source.Git != nil {
		result.Git = &agentplugin.GitSource{URL: source.Git.URL, Commit: source.Git.Commit}
		if ref := source.Git.CredentialRef; ref != nil {
			sum := sha256.Sum256([]byte(namespace + "\x00" + ref.Name + "\x00" + ref.Key))
			name := ArtifactCredentialEnvPrefix + strings.ToUpper(fmt.Sprintf("%x", sum[:8]))
			credential = &corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: ref.DeepCopy()}}
		}
	}
	if source.Bucket != nil {
		result.S3 = &agentplugin.S3Source{
			Endpoint:  source.Bucket.S3.Endpoint,
			Bucket:    source.Bucket.S3.Bucket,
			Key:       source.Bucket.S3.Key,
			VersionID: source.Bucket.S3.VersionID,
			Region:    source.Bucket.S3.Region,
		}
	}
	return result, credential
}

func appendArtifactSourceDestination(destinations []string, source agentplugin.Source) []string {
	switch {
	case source.Git != nil:
		return appendURLHostname(destinations, source.Git.URL)
	case source.OCI != "":
		repository := strings.SplitN(source.OCI, "@", 2)[0]
		first, _, found := strings.Cut(repository, "/")
		if found && (strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost") {
			return append(destinations, first)
		}
		return append(destinations, "registry-1.docker.io")
	case source.S3 != nil:
		return appendURLHostname(destinations, source.S3.Endpoint)
	default:
		return destinations
	}
}

func appendURLHostname(destinations []string, rawURL string) []string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Hostname() != "" {
		return append(destinations, parsed.Hostname())
	}
	return destinations
}
