package translator

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

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
		// Plugins contribute runtime configuration and cannot defer fetching.
		if plugin.Source.Git != nil && plugin.Source.Git.AuthorizationFrom != nil {
			return nil, NewValidationError("authorizationFrom is supported only for skills[].source.git")
		}
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
		result.Git = &agentplugin.GitSource{URL: source.Git.URL, Commit: source.Git.Commit, GatewayAuthorization: source.Git.AuthorizationFrom != nil}
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

// resolveGitSkillCredentials tracks Secret dependencies before emitting any
// runtime credential binding. Template authors cannot grant Secret authority.
func (c *Compiler) resolveGitSkillCredentials(template *TemplateConfiguration) error {
	for _, skill := range template.Spec.Skills {
		git := skill.Source.Git
		if git == nil || git.AuthorizationFrom == nil {
			continue
		}
		origin, err := gitCredentialOrigin(git.URL)
		if err != nil {
			return err
		}
		ref := git.AuthorizationFrom
		key := types.NamespacedName{Namespace: template.Namespace, Name: ref.Name}
		found := krt.FetchOne(c.ctx, c.collections.Secrets, krt.FilterObjectName(key))
		if found == nil {
			return fmt.Errorf("resolve skill %q authorizationFrom: Secret %q not found", skill.Name, key)
		}
		secret := *found
		if len(secret.Data[ref.Key]) == 0 {
			return fmt.Errorf("resolve skill %q authorizationFrom: Secret %q does not contain non-empty key %q", skill.Name, key, ref.Key)
		}
		if err := authorizeGitCredentialOrigin(secret, origin); err != nil {
			return fmt.Errorf("resolve skill %q authorizationFrom: %w", skill.Name, err)
		}
	}
	return nil
}

func gitCredentialOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery {
		return "", NewValidationError("Git authorizationFrom requires an absolute HTTPS URL without user information, query, or fragment")
	}
	origin, err := egress.ParseOrigin(egress.Origin(u))
	if err != nil || strings.Contains(u.Hostname(), "*") {
		return "", NewValidationError("Git authorizationFrom requires an exact HTTPS DNS origin")
	}
	return origin.String(), nil
}

func authorizeGitCredentialOrigin(secret *corev1.Secret, origin string) error {
	allowed, err := egress.ParseOrigin(secret.Annotations[v1alpha3.GitCredentialOriginAnnotation])
	if err != nil || allowed.Scheme != "https" || strings.Contains(allowed.Host, "*") || allowed.String() != origin {
		return fmt.Errorf("secret %q must authorize origin %q with annotation %q", secret.Name, origin, v1alpha3.GitCredentialOriginAnnotation)
	}
	return nil
}
