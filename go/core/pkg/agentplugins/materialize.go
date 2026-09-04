package agentplugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/agentplugin"
	"github.com/kagent-dev/kagent/go/core/internal/skillsinit"
	"github.com/kagent-dev/kagent/go/internal/logging"
)

const (
	maxPackageBytes   = 100 << 20
	maxPackageEntries = 10_000
	pluginSchema      = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	mcpSchema         = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
)

var pluginNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

// Paths contains the package cache and selected-skill destinations.
type Paths struct {
	Packages string
	Skills   string
}

// Materialization is the runtime-neutral result of materializing Agent Plugin
// resources. Its plugin package locations remain private to this package.
type Materialization struct {
	SkillsDirectory string
	plugins         []materializedPlugin
}

// MCPConfig contains MCP servers resolved from materialized plugins.
type MCPConfig struct {
	StreamableHTTP []RemoteMCPServer
	SSE            []RemoteMCPServer
	Stdio          []StdioMCPServer
}

// RemoteMCPServer is an HTTP-based MCP server resolved from mcp.json.
type RemoteMCPServer struct {
	URL     string
	Headers map[string]string
}

// StdioMCPServer is a local MCP server resolved from mcp.json.
type StdioMCPServer struct {
	Command string
	Args    []string
	Env     map[string]string
	Dir     string
}

// MaterializedPlugin is a plugin that has been materialized.
type materializedPlugin struct {
	name string
	root string
}

// Materialize fetches Agent Plugin resources and copies explicitly selected
// skills into their runtime directory. It does not load runtime configuration.
func Materialize(ctx context.Context, resources agentplugin.Resources, paths Paths) (Materialization, error) {
	plugins, err := materializeResources(ctx, resources, paths)
	if err != nil {
		return Materialization{}, err
	}
	return Materialization{SkillsDirectory: paths.Skills, plugins: plugins}, nil
}

// LoadMCP resolves standard MCP configuration from a materialization. The
// caller chooses the runtime-owned root for mutable plugin data.
func LoadMCP(ctx context.Context, materialization Materialization, dataRoot string) (MCPConfig, error) {
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		return MCPConfig{}, fmt.Errorf("create plugin data directory: %w", err)
	}
	var result MCPConfig
	for _, plugin := range materialization.plugins {
		mcp := loadMCP(ctx, plugin.root, filepath.Join(dataRoot, plugin.name))
		result.StreamableHTTP = append(result.StreamableHTTP, mcp.StreamableHTTP...)
		result.SSE = append(result.SSE, mcp.SSE...)
		result.Stdio = append(result.Stdio, mcp.Stdio...)
	}
	return result, nil
}

func materializeResources(ctx context.Context, resources agentplugin.Resources, paths Paths) ([]materializedPlugin, error) {
	selectedSkills := make([]string, 0, len(resources.Skills))
	for _, skill := range resources.Skills {
		selectedSkills = append(selectedSkills, skill.Name)
	}
	for _, plugin := range resources.Plugins {
		selectedSkills = append(selectedSkills, plugin.Skills...)
	}
	if err := validateSkillSelections(selectedSkills); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(paths.Skills, 0o755); err != nil {
		return nil, fmt.Errorf("create skills directory: %w", err)
	}
	if err := os.MkdirAll(paths.Packages, 0o755); err != nil {
		return nil, fmt.Errorf("create package directory: %w", err)
	}

	for i, skill := range resources.Skills {
		root := filepath.Join(paths.Packages, fmt.Sprintf("standalone-%d", i))
		sourceRoot, err := fetchSource(ctx, skill.Source, root, "SKILL.md")
		if err != nil {
			return nil, fmt.Errorf("materialize skill %q: %w", skill.Name, err)
		}
		if err := copySkill(sourceRoot, filepath.Join(paths.Skills, skill.Name)); err != nil {
			return nil, fmt.Errorf("materialize skill %q: %w", skill.Name, err)
		}
	}

	pluginNames := make(map[string]struct{})
	plugins := make([]materializedPlugin, 0, len(resources.Plugins))
	for i, plugin := range resources.Plugins {
		root := filepath.Join(paths.Packages, fmt.Sprintf("plugin-%d", i))
		pluginRoot, err := fetchSource(ctx, plugin.Source, root, "plugin.json")
		if err != nil {
			return nil, fmt.Errorf("materialize plugin %d: %w", i, err)
		}
		manifest, err := loadManifest(pluginRoot)
		if err != nil {
			return nil, fmt.Errorf("load plugin %d: %w", i, err)
		}
		if _, exists := pluginNames[manifest.Name]; exists {
			return nil, fmt.Errorf("duplicate plugin name %q", manifest.Name)
		}
		pluginNames[manifest.Name] = struct{}{}
		for _, name := range plugin.Skills {
			source := filepath.Join(pluginRoot, "skills", name)
			if err := copySkill(source, filepath.Join(paths.Skills, name)); err != nil {
				return nil, fmt.Errorf("plugin %q skill %q: %w", manifest.Name, name, err)
			}
		}
		plugins = append(plugins, materializedPlugin{name: manifest.Name, root: pluginRoot})
	}
	return plugins, nil
}

func validateSkillSelections(names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if err := validateSkillName(name); err != nil {
			return err
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate skill name %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validateSkillName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("skill name %q must be a single relative path component", name)
	}
	return nil
}

func fetchSource(ctx context.Context, source agentplugin.Source, destination, requiredFile string) (string, error) {
	selected := 0
	if source.OCI != "" {
		selected++
	}
	if source.Git != nil {
		selected++
	}
	if source.S3 != nil {
		selected++
	}
	if selected != 1 {
		return "", fmt.Errorf("exactly one artifact source is required")
	}
	if _, err := os.Stat(destination); err == nil {
		if err := validatePackage(destination); err != nil {
			return "", err
		}
		root, err := containedPath(destination, source.Path)
		if err == nil {
			if info, err := os.Stat(filepath.Join(root, requiredFile)); err == nil && info.Mode().IsRegular() {
				return root, nil
			} else if err != nil && !os.IsNotExist(err) {
				return "", err
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.RemoveAll(destination); err != nil {
		return "", err
	}
	switch {
	case source.OCI != "":
		if err := skillsinit.FetchOCI(skillsinit.OCIRef{Image: source.OCI, Dest: destination}, false); err != nil {
			return "", err
		}
	case source.Git != nil:
		if err := skillsinit.CloneGitCommit(source.Git.URL, source.Git.Commit, destination); err != nil {
			return "", err
		}
	case source.S3 != nil:
		ref := skillsinit.S3Ref{
			URI: "s3://" + source.S3.Bucket + "/" + source.S3.Key, Dest: destination,
			Endpoint: source.S3.Endpoint, Region: source.S3.Region, VersionID: source.S3.VersionID,
		}
		if err := skillsinit.FetchS3(ctx, ref); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("artifact source is required")
	}
	if err := validatePackage(destination); err != nil {
		return "", err
	}
	root, err := containedPath(destination, source.Path)
	if err != nil {
		return "", err
	}
	return root, nil
}

func validatePackage(root string) error {
	var entries, bytes int64
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		entries++
		if entries > maxPackageEntries {
			return fmt.Errorf("artifact contains more than %d filesystem entries", maxPackageEntries)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
			if bytes > maxPackageBytes {
				return fmt.Errorf("artifact exceeds %d bytes", maxPackageBytes)
			}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			if !pathWithin(root, resolved) {
				return fmt.Errorf("symlink %q escapes artifact root", path)
			}
		}
		return nil
	})
}

func containedPath(root, relative string) (string, error) {
	path := root
	if relative != "" {
		if filepath.IsAbs(relative) {
			return "", fmt.Errorf("artifact path %q is not relative", relative)
		}
		if slices.Contains(strings.Split(filepath.ToSlash(relative), "/"), "..") {
			return "", fmt.Errorf("artifact path %q is not relative", relative)
		}
		path = filepath.Join(root, filepath.FromSlash(relative))
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !pathWithin(root, resolved) {
		return "", fmt.Errorf("artifact path %q escapes its root", relative)
	}
	return resolved, nil
}

func pathWithin(root, path string) bool {
	root = canonicalPath(root)
	path = canonicalPath(path)
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func canonicalPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(canonicalPath(parent), filepath.Base(path))
}

func copySkill(source, destination string) error {
	info, err := os.Stat(filepath.Join(source, "SKILL.md"))
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("SKILL.md is required")
	}
	if err := os.RemoveAll(destination); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if filepath.IsAbs(link) {
				return fmt.Errorf("skill symlink %q must be relative", path)
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			if !pathWithin(source, resolved) {
				return fmt.Errorf("skill symlink %q escapes skill root", path)
			}
			return os.Symlink(link, target)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

type manifest struct {
	Schema      string                     `json:"$schema"`
	Name        string                     `json:"name"`
	Version     string                     `json:"version,omitempty"`
	Description string                     `json:"description,omitempty"`
	Author      *manifestAuthor            `json:"author,omitempty"`
	Homepage    string                     `json:"homepage,omitempty"`
	Repository  string                     `json:"repository,omitempty"`
	License     string                     `json:"license,omitempty"`
	Keywords    []string                   `json:"keywords,omitempty"`
	Extensions  map[string]json.RawMessage `json:"extensions,omitempty"`
}

type manifestAuthor struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

func loadManifest(root string) (manifest, error) {
	raw, err := os.ReadFile(filepath.Join(root, "plugin.json"))
	if err != nil {
		return manifest{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return manifest{}, err
	}
	allowed := map[string]bool{
		"$schema": true, "name": true, "version": true, "description": true, "author": true,
		"homepage": true, "repository": true, "license": true, "keywords": true, "extensions": true,
	}
	for name := range fields {
		if !allowed[name] {
			delete(fields, name)
		}
	}
	if rawExtensions := fields["extensions"]; len(rawExtensions) > 0 && rawExtensions[0] != '{' {
		delete(fields, "extensions")
	}
	validated, err := json.Marshal(fields)
	if err != nil {
		return manifest{}, err
	}
	var value manifest
	decoder := json.NewDecoder(strings.NewReader(string(validated)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return manifest{}, err
	}
	if value.Schema != pluginSchema {
		return manifest{}, fmt.Errorf("unsupported plugin schema %q", value.Schema)
	}
	if len(value.Name) > 64 || !pluginNamePattern.MatchString(value.Name) || strings.Contains(value.Name, "--") || strings.Contains(value.Name, "..") {
		return manifest{}, fmt.Errorf("invalid plugin name %q", value.Name)
	}
	return value, nil
}

type mcpDocument struct {
	Schema  string                     `json:"$schema"`
	Servers map[string]json.RawMessage `json:"mcpServers"`
}

type mcpServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	CWD     string            `json:"cwd,omitempty"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func loadMCP(ctx context.Context, root, dataRoot string) MCPConfig {
	raw, err := os.ReadFile(filepath.Join(root, "mcp.json"))
	if os.IsNotExist(err) {
		return MCPConfig{}
	}
	logger := logging.FromContext(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "unable to read plugin MCP configuration", "error", err, "plugin_root", root)
		return MCPConfig{}
	}
	var document mcpDocument
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		logger.ErrorContext(ctx, "invalid plugin MCP configuration", "error", err, "plugin_root", root)
		return MCPConfig{}
	}
	if document.Schema != mcpSchema {
		logger.ErrorContext(ctx, "invalid plugin MCP configuration", "error", fmt.Errorf("unsupported MCP schema %q", document.Schema), "plugin_root", root)
		return MCPConfig{}
	}
	if document.Servers == nil {
		logger.ErrorContext(ctx, "invalid plugin MCP configuration", "error", fmt.Errorf("mcpServers is required"), "plugin_root", root)
		return MCPConfig{}
	}
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		logger.ErrorContext(ctx, "unable to create plugin data directory", "error", err, "plugin_root", root)
		return MCPConfig{}
	}
	var result MCPConfig
	names := make([]string, 0, len(document.Servers))
	for name := range document.Servers {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		rawServer := document.Servers[name]
		server, err := parseMCPServer(rawServer, root, dataRoot)
		if err != nil {
			logger.ErrorContext(ctx, "ignoring invalid plugin MCP server", "error", err, "plugin_root", root, "server", name)
			continue
		}
		switch server.Type {
		case "stdio":
			result.Stdio = append(result.Stdio, StdioMCPServer{Command: server.Command, Args: server.Args, Env: server.Env, Dir: server.CWD})
		case "streamable-http":
			result.StreamableHTTP = append(result.StreamableHTTP, RemoteMCPServer{URL: server.URL, Headers: server.Headers})
		case "sse":
			result.SSE = append(result.SSE, RemoteMCPServer{URL: server.URL, Headers: server.Headers})
		}
	}
	return result
}

func parseMCPServer(raw json.RawMessage, root, dataRoot string) (mcpServer, error) {
	var server mcpServer
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&server); err != nil {
		return mcpServer{}, err
	}
	switch server.Type {
	case "stdio":
		if server.Command == "" || server.URL != "" || server.Headers != nil || strings.ContainsAny(server.Command, " \t\r\n") {
			return mcpServer{}, fmt.Errorf("invalid stdio server")
		}
		relativeCommand, pluginRelative := strings.CutPrefix(server.Command, "./")
		if strings.ContainsRune(server.Command, filepath.Separator) && !pluginRelative {
			return mcpServer{}, fmt.Errorf("stdio command must be bare or plugin-relative")
		}
		if pluginRelative {
			command, err := containedPath(root, relativeCommand)
			if err != nil {
				return mcpServer{}, err
			}
			server.Command = command
		}
		for key := range server.Env {
			if key == "PLUGIN_ROOT" || key == "PLUGIN_DATA" {
				return mcpServer{}, fmt.Errorf("stdio environment cannot override %s", key)
			}
		}
		if server.Env == nil {
			server.Env = make(map[string]string)
		}
		server.Args = expandAll(server.Args, root, dataRoot)
		for key, value := range server.Env {
			server.Env[key] = expand(value, root, dataRoot)
		}
		server.Env["PLUGIN_ROOT"], server.Env["PLUGIN_DATA"] = root, dataRoot
		if server.CWD == "" {
			server.CWD = root
		} else {
			switch {
			case strings.HasPrefix(server.CWD, "./"):
				server.CWD = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(server.CWD, "./")))
			case strings.HasPrefix(server.CWD, "${PLUGIN_ROOT}"), strings.HasPrefix(server.CWD, "${PLUGIN_DATA}"):
				server.CWD = expand(server.CWD, root, dataRoot)
			default:
				return mcpServer{}, fmt.Errorf("stdio cwd must be plugin-relative or use PLUGIN_ROOT or PLUGIN_DATA")
			}
			if !pathWithin(root, server.CWD) && !pathWithin(dataRoot, server.CWD) {
				return mcpServer{}, fmt.Errorf("stdio cwd escapes plugin roots")
			}
			if pathWithin(dataRoot, server.CWD) {
				if err := os.MkdirAll(server.CWD, 0o755); err != nil {
					return mcpServer{}, err
				}
			}
		}
	case "streamable-http", "sse":
		if server.URL == "" || server.Command != "" || server.Args != nil || server.Env != nil || server.CWD != "" {
			return mcpServer{}, fmt.Errorf("invalid remote server")
		}
		parsed, err := url.Parse(server.URL)
		if err != nil || parsed.User != nil || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return mcpServer{}, fmt.Errorf("invalid remote MCP URL %q", server.URL)
		}
		host := parsed.Hostname()
		if parsed.Scheme != "https" && host != "localhost" && !net.ParseIP(host).IsLoopback() {
			return mcpServer{}, fmt.Errorf("non-loopback remote MCP URL must use HTTPS")
		}
	default:
		return mcpServer{}, fmt.Errorf("unsupported MCP transport %q", server.Type)
	}
	return server, nil
}

func expand(value, root, dataRoot string) string {
	value = strings.ReplaceAll(value, "${PLUGIN_ROOT}", root)
	return strings.ReplaceAll(value, "${PLUGIN_DATA}", dataRoot)
}

func expandAll(values []string, root, dataRoot string) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = expand(value, root, dataRoot)
	}
	return result
}
