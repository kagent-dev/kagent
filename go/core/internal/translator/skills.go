package translator

import (
	"crypto/sha256"
	"fmt"
	"net/url"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

// SkillImage is a normalized, digest-pinned artifact mounted for skill loading.
type SkillImage string

// MountPath identifies the same source across skills, plugins, and shared agents.
func (s SkillImage) MountPath() string {
	return fmt.Sprintf("/run/kagent/artifacts/%x", sha256.Sum256([]byte(s)))
}

// SkillCompilation separates runtime selections from platform delivery inputs.
type SkillCompilation struct {
	Resources agentplugin.Resources
	Images    []SkillImage
	Egress    []string
}

// CompileSkillResources translates portable AgentTemplate skill selections
// into the runtime-neutral resource contract shared by Harness adapters.
func CompileSkillResources(template *TemplateConfiguration) (*SkillCompilation, error) {
	result := &SkillCompilation{Resources: agentplugin.Resources{
		Skills:  make([]agentplugin.Skill, 0, len(template.Spec.Skills)),
		Plugins: make([]agentplugin.Bundle, 0, len(template.Spec.Plugins)),
	}}
	selected := make(map[string]struct{})
	for _, skill := range template.Spec.Skills {
		if _, exists := selected[skill.Name]; exists {
			return nil, NewValidationError("duplicate skill name %q", skill.Name)
		}
		selected[skill.Name] = struct{}{}
		source, err := result.compileSource(skill.Source)
		if err != nil {
			return nil, fmt.Errorf("compile skill %q: %w", skill.Name, err)
		}
		result.Resources.Skills = append(result.Resources.Skills, agentplugin.Skill{Name: skill.Name, Source: source})
	}
	for i, plugin := range template.Spec.Plugins {
		for _, name := range plugin.Skills {
			if _, exists := selected[name]; exists {
				return nil, NewValidationError("duplicate skill name %q", name)
			}
			selected[name] = struct{}{}
		}
		source, err := result.compileSource(plugin.Source)
		if err != nil {
			return nil, fmt.Errorf("compile plugin %d: %w", i, err)
		}
		result.Resources.Plugins = append(result.Resources.Plugins, agentplugin.Bundle{
			Source: source,
			Skills: append([]string(nil), plugin.Skills...),
		})
	}
	return result, nil
}

func (s *SkillCompilation) compileSource(source v1alpha3.ArtifactSource) (agentplugin.Source, error) {
	result := agentplugin.Source{Path: source.Path}
	if source.OCI != "" {
		ref, err := name.NewDigest(source.OCI)
		if err != nil {
			return result, NewValidationError("invalid OCI skill image %q: %v", source.OCI, err)
		}
		image := SkillImage(ref.Name())
		if len(image) > 512 {
			return result, NewValidationError("OCI skill image exceeds Substrate's 512-character reference limit")
		}
		result.Directory = image.MountPath()
		s.Images = append(s.Images, image)
	}
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
	s.Egress = appendArtifactSourceDestination(s.Egress, result)
	return result, nil
}

func appendArtifactSourceDestination(destinations []string, source agentplugin.Source) []string {
	switch {
	case source.Git != nil:
		return appendURLOrigin(destinations, source.Git.URL)
	case source.S3 != nil:
		return appendURLOrigin(destinations, source.S3.Endpoint)
	default:
		return destinations
	}
}

func appendURLOrigin(destinations []string, rawURL string) []string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Hostname() != "" {
		return append(destinations, egress.Origin(parsed))
	}
	return destinations
}
