package adk

import (
	"github.com/kagent-dev/kagent/go/api/agentplugin"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// PluginConfig converts the shared Harness compilation result to ADK input.
func PluginConfig(resources agentplugin.Resources) *apiv1.ADKAgentPlugins {
	result := &apiv1.ADKAgentPlugins{}
	for _, skill := range resources.Skills {
		result.Skills = append(result.Skills, &apiv1.ADKSkill{Name: skill.Name, Source: pluginSource(skill.Source)})
	}
	for _, bundle := range resources.Plugins {
		result.Plugins = append(result.Plugins, &apiv1.ADKPluginBundle{Source: pluginSource(bundle.Source), Skills: bundle.Skills})
	}
	return result
}
func pluginSource(source agentplugin.Source) *apiv1.ADKPluginSource {
	result := &apiv1.ADKPluginSource{Path: source.Path}
	switch {
	case source.OCI != "":
		result.Source = &apiv1.ADKPluginSource_Oci{Oci: source.OCI}
	case source.Git != nil:
		result.Source = &apiv1.ADKPluginSource_Git{Git: &apiv1.ADKPluginGitSource{Url: source.Git.URL, Commit: source.Git.Commit}}
	case source.S3 != nil:
		result.Source = &apiv1.ADKPluginSource_S3{S3: &apiv1.ADKPluginS3Source{Endpoint: source.S3.Endpoint, Bucket: source.S3.Bucket, Key: source.S3.Key, VersionId: source.S3.VersionID, Region: source.S3.Region}}
	}
	return result
}

// PluginResources adapts validated ADK input to the runtime-neutral materializer.
func PluginResources(config *apiv1.ADKAgentPlugins) agentplugin.Resources {
	result := agentplugin.Resources{}
	for _, skill := range config.GetSkills() {
		result.Skills = append(result.Skills, agentplugin.Skill{Name: skill.Name, Source: pluginResourceSource(skill.Source)})
	}
	for _, bundle := range config.GetPlugins() {
		result.Plugins = append(result.Plugins, agentplugin.Bundle{Source: pluginResourceSource(bundle.Source), Skills: bundle.Skills})
	}
	return result
}
func pluginResourceSource(source *apiv1.ADKPluginSource) agentplugin.Source {
	result := agentplugin.Source{Path: source.GetPath()}
	switch value := source.GetSource().(type) {
	case *apiv1.ADKPluginSource_Oci:
		result.OCI = value.Oci
	case *apiv1.ADKPluginSource_Git:
		result.Git = &agentplugin.GitSource{URL: value.Git.Url, Commit: value.Git.Commit}
	case *apiv1.ADKPluginSource_S3:
		result.S3 = &agentplugin.S3Source{Endpoint: value.S3.Endpoint, Bucket: value.S3.Bucket, Key: value.S3.Key, VersionID: value.S3.VersionId, Region: value.S3.Region}
	}
	return result
}
