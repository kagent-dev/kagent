// Package config reads the ADK-shaped configuration Kagent compiles for a BYO
// Harness and narrows it to what the omp adapter implements. A field the
// adapter cannot honour is refused with the reason rather than dropped, so an
// Agent never runs with less than its definition asks for.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/agentplugin"
)

const (
	// PinnedOmpVersion is the omp release the image installs; the adapter
	// refuses any other binary, so a protocol change arrives only with a
	// reviewed bump. Renovate moves it with the Dockerfile's OMP_VERSION.
	PinnedOmpVersion = "18.12.1"
)

// Config is the part of the compiled Agent the omp adapter runs.
type Config struct {
	Model       Model
	Instruction string
	// MCPServers are the Agent's Streamable HTTP servers in compiled order,
	// each under a stable name omp shows the model as the tool prefix.
	MCPServers []MCPServer
	Skills     *agentplugin.Resources
}

// Model is an Anthropic Messages endpoint. The API key never appears here: omp
// reads it from the process environment, where the controller has put the
// placeholder the egress gateway replaces with the Secret.
type Model struct {
	ID        string
	BaseURL   string
	Headers   map[string]string
	MaxTokens int
}

// MCPServer is one Streamable HTTP server. Header values are final: a
// Secret-backed header already holds the placeholder the egress gateway
// replaces on the server's origin.
type MCPServer struct {
	Name    string
	URL     string
	Headers map[string]string
	// HeaderVariables are the environment variables the headers resolved
	// from; the bridge holds their values, so omp's environment need not.
	HeaderVariables []string
	Tools           []string
	TimeoutSeconds  float64
}

// DefaultAnthropicBaseURL is the endpoint of a ModelConfig without baseUrl.
const DefaultAnthropicBaseURL = "https://api.anthropic.com"

const envTokenPrefix, envTokenSuffix = "__KAGENT_ENV[", "]__"

var serverNameInvalid = regexp.MustCompile(`[^a-z0-9_]+`)

// Parse decodes KAGENT_CONFIG_JSON. lookupEnv resolves the __KAGENT_ENV[NAME]__
// tokens the compiler writes for Secret-backed headers.
func Parse(raw []byte, lookupEnv func(string) (string, bool)) (Config, error) {
	var agent adk.AgentConfig
	if err := json.Unmarshal(raw, &agent); err != nil {
		return Config{}, fmt.Errorf("decode agent configuration: %w", err)
	}
	if err := refuseUnsupported(&agent); err != nil {
		return Config{}, err
	}
	model, err := parseModel(agent.Model, lookupEnv)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{Model: model, Instruction: agent.Instruction, Skills: agent.AgentPlugins}
	if cfg.Skills != nil && len(cfg.Skills.Plugins) > 0 {
		return Config{}, fmt.Errorf("plugins are not supported by the omp Harness; bind the plugin's skills instead")
	}
	used := map[string]int{}
	for index, tool := range agent.HttpTools {
		server, err := parseMCPServer(index, tool, lookupEnv)
		if err != nil {
			return Config{}, err
		}
		server.Name = uniqueServerName(server.URL, used)
		cfg.MCPServers = append(cfg.MCPServers, server)
	}
	return cfg, nil
}

func refuseUnsupported(agent *adk.AgentConfig) error {
	switch {
	case len(agent.SseTools) > 0:
		return fmt.Errorf("SSE MCP servers are not supported by the omp Harness; use Streamable HTTP")
	case len(agent.StdioTools) > 0:
		return fmt.Errorf("stdio MCP servers are not supported by the omp Harness")
	case len(agent.RemoteAgents) > 0 || len(agent.SubAgents) > 0:
		return fmt.Errorf("agent bindings are not supported by the omp Harness")
	case agent.Memory != nil:
		return fmt.Errorf("memory is not supported by the omp Harness")
	case agent.Output != nil:
		return fmt.Errorf("structured output is not supported by the omp Harness")
	}
	return nil
}

func parseModel(model adk.Model, lookupEnv func(string) (string, bool)) (Model, error) {
	anthropic, ok := model.(*adk.Anthropic)
	if !ok || anthropic == nil {
		kind := "none"
		if model != nil {
			kind = model.GetType()
		}
		return Model{}, fmt.Errorf("the omp Harness needs an Anthropic ModelConfig, got %q", kind)
	}
	switch {
	case strings.TrimSpace(anthropic.Model) == "":
		return Model{}, fmt.Errorf("the Anthropic ModelConfig names no model")
	case anthropic.APIKeyPassthrough:
		return Model{}, fmt.Errorf("API key passthrough is not supported by the omp Harness")
	case anthropic.TLSInsecureSkipVerify != nil && *anthropic.TLSInsecureSkipVerify,
		anthropic.TLSCACertPath != nil && *anthropic.TLSCACertPath != "",
		anthropic.TLSDisableSystemCAs != nil && *anthropic.TLSDisableSystemCAs:
		return Model{}, fmt.Errorf("custom model TLS is not supported by the omp Harness")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(anthropic.BaseUrl), "/")
	if baseURL == "" {
		baseURL = DefaultAnthropicBaseURL
	}
	if err := requireHTTPURL("model base URL", baseURL); err != nil {
		return Model{}, err
	}
	headers, _, err := expandHeaders(anthropic.Headers, lookupEnv)
	if err != nil {
		return Model{}, fmt.Errorf("model headers: %w", err)
	}
	result := Model{ID: anthropic.Model, BaseURL: baseURL, Headers: headers}
	if anthropic.MaxTokens != nil {
		result.MaxTokens = *anthropic.MaxTokens
	}
	return result, nil
}

func parseMCPServer(index int, tool adk.HttpMcpServerConfig, lookupEnv func(string) (string, bool)) (MCPServer, error) {
	params := tool.Params
	switch {
	case tool.RequireApproval:
		return MCPServer{}, fmt.Errorf("MCP server %d: tool approval is not supported by the omp Harness", index)
	case params.TLSInsecureSkipVerify != nil && *params.TLSInsecureSkipVerify,
		params.TLSCACertPath != nil && *params.TLSCACertPath != "",
		params.TLSDisableSystemCAs != nil && *params.TLSDisableSystemCAs:
		return MCPServer{}, fmt.Errorf("MCP server %d: custom TLS is not supported by the omp Harness", index)
	}
	if err := requireHTTPURL(fmt.Sprintf("MCP server %d URL", index), params.Url); err != nil {
		return MCPServer{}, err
	}
	headers, variables, err := expandHeaders(params.Headers, lookupEnv)
	if err != nil {
		return MCPServer{}, fmt.Errorf("MCP server %d headers: %w", index, err)
	}
	server := MCPServer{URL: params.Url, Headers: headers, HeaderVariables: variables, Tools: tool.Tools}
	if params.Timeout != nil {
		server.TimeoutSeconds = *params.Timeout
	}
	return server, nil
}

// expandHeaders resolves the compiler's __KAGENT_ENV[NAME]__ tokens and
// returns the variables it read, sorted. Kagent sets a Secret-backed variable
// to the egress gateway's placeholder, so the resolved value is the
// placeholder, never the Secret.
func expandHeaders(headers map[string]string, lookupEnv func(string) (string, bool)) (map[string]string, []string, error) {
	if len(headers) == 0 {
		return nil, nil, nil
	}
	result := make(map[string]string, len(headers))
	var variables []string
	for name, value := range headers {
		if strings.HasPrefix(value, envTokenPrefix) && strings.HasSuffix(value, envTokenSuffix) {
			variable := strings.TrimSuffix(strings.TrimPrefix(value, envTokenPrefix), envTokenSuffix)
			resolved, ok := lookupEnv(variable)
			if !ok {
				return nil, nil, fmt.Errorf("header %q: environment variable %s is not set", name, variable)
			}
			value = resolved
			variables = append(variables, variable)
		}
		result[name] = value
	}
	slices.Sort(variables)
	return result, slices.Compact(variables), nil
}

func requireHTTPURL(what, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL", what)
	}
	return nil
}

// uniqueServerName names a server after the first label of its host, so a
// muster server's tools read mcp__muster_* to the model. The ADK configuration
// carries no server name; a second server on the same label gets a suffix.
// The name is already in the form omp's tool names use, so the adapter can
// spell them.
func uniqueServerName(rawURL string, used map[string]int) string {
	name := "mcp"
	if u, err := url.Parse(rawURL); err == nil {
		label, _, _ := strings.Cut(u.Hostname(), ".")
		if label = strings.Trim(serverNameInvalid.ReplaceAllString(strings.ToLower(label), "_"), "_"); label != "" {
			name = label
		}
	}
	used[name]++
	if count := used[name]; count > 1 {
		return fmt.Sprintf("%s_%d", name, count)
	}
	return name
}
