package translator

import (
	"net/url"
	"strings"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// CompileSkillResources translates portable AgentTemplate skill selections
// into the runtime-neutral resource contract shared by Harness adapters.
func CompileSkillResources(template *v1alpha3.AgentTemplate) (agentplugin.Resources, []string, error) {
	resources := agentplugin.Resources{
		Skills:  make([]agentplugin.Skill, 0, len(template.Spec.Skills)),
		Plugins: make([]agentplugin.Bundle, 0, len(template.Spec.Plugins)),
	}
	selected := make(map[string]struct{})
	var egress []string
	for _, skill := range template.Spec.Skills {
		if _, exists := selected[skill.Name]; exists {
			return agentplugin.Resources{}, nil, NewValidationError("duplicate skill name %q", skill.Name)
		}
		selected[skill.Name] = struct{}{}
		source := compileArtifactSource(skill.Source)
		resources.Skills = append(resources.Skills, agentplugin.Skill{Name: skill.Name, Source: source})
		egress = appendArtifactSourceDestination(egress, source)
	}
	for _, plugin := range template.Spec.Plugins {
		for _, name := range plugin.Skills {
			if _, exists := selected[name]; exists {
				return agentplugin.Resources{}, nil, NewValidationError("duplicate skill name %q", name)
			}
			selected[name] = struct{}{}
		}
		source := compileArtifactSource(plugin.Source)
		resources.Plugins = append(resources.Plugins, agentplugin.Bundle{
			Source: source,
			Skills: append([]string(nil), plugin.Skills...),
		})
		egress = appendArtifactSourceDestination(egress, source)
	}
	return resources, egress, nil
}

func compileArtifactSource(source v1alpha3.ArtifactSource) agentplugin.Source {
	result := agentplugin.Source{OCI: source.OCI, Path: source.Path}
	if source.Git != nil {
		result.Git = &agentplugin.GitSource{URL: source.Git.URL, Commit: source.Git.Commit}
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
	return result
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
