package models

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/ollama/ollama/api"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// OllamaConfig holds Ollama configuration
type OllamaConfig struct {
	TransportConfig
	Model   string
	Host    string            // Ollama server host (e.g., http://localhost:11434)
	APIKey  string            // Gateway placeholder for Ollama Cloud; empty for a local daemon
	Options map[string]string // Ollama-specific options (temperature, top_p, num_ctx, etc.)
}

// Ollama's two endpoints, mirroring the routing rules in pi-go. Which one a
// model reaches is decided by its tag plus whether a key is set — never by the
// key alone.
const (
	ollamaLocalURL = "http://localhost:11434"
	// api.ollama.com, not ollama.com. The Ollama Go SDK sends a request to
	// ollama.com with an Authorization header it derives from a locally signed
	// nonce, and returns the signing error before the request leaves the
	// process when that key is absent — as it is in an agent pod. api.ollama.com
	// misses that special case, so the Bearer token we set is the one used.
	ollamaCloudURL = "https://api.ollama.com"
)

// OllamaCloudModels names the models served by api.ollama.com, as returned by
// GET https://api.ollama.com/api/tags. This is the single source of truth for
// cloud membership: the controller's model catalog renders its Ollama cloud
// entries from it, and isOllamaCloudModel consults it, so the list a user picks
// from and the rule that routes their choice cannot drift apart.
//
// A bare catalog name is a cloud model. The ":cloud" suffix is also accepted
// (see isOllamaCloudModel) because a signed-in local daemon proxies the same
// model under that tag.
var OllamaCloudModels = []string{
	"kimi-k2.6",
	"kimi-k2.7-code",
	"kimi-k3",
	"glm-5.1",
	"glm-5.2",
	"glm-5.3",
	"glm-5.3-flash",
	"minimax-m2.7",
	"minimax-m3",
	"deepseek-v4.1-flash",
	"deepseek-v4-flash:0731",
	"deepseek-v4-pro:0813",
	"gpt-oss:20b",
	"gpt-oss:120b",
	"qwen3.5:397b",
	"mistral-large-3:675b",
	"nemotron-3-nano:30b",
	"nemotron-3-super",
	"nemotron-3-ultra",
	"gemma4:31b",
}

// ollamaCloudModelSet is OllamaCloudModels as a lookup table.
var ollamaCloudModelSet = func() map[string]bool {
	set := make(map[string]bool, len(OllamaCloudModels))
	for _, name := range OllamaCloudModels {
		set[name] = true
	}
	return set
}()

// isOllamaCloudModel reports whether an Ollama model name is a cloud model.
//
// A name is cloud when it is one of the catalog's cloud models, or when it
// carries a ":cloud"/"-cloud" tag. Both forms have to count: the catalog lists
// bare names because that is what the cloud API accepts, while users and docs
// also write the tagged form, and a cloud model selected from the catalog used
// to be treated as local — it either called the pod's own localhost:11434 or
// failed compilation on a credential that had nowhere to bind.
//
// The tag rule is a suffix test on purpose, so a name that merely contains
// "cloud" stays local (cloudy-llm:7b is not a cloud model).
func isOllamaCloudModel(modelName string) bool {
	if ollamaCloudModelSet[modelName] {
		return true
	}
	return strings.HasSuffix(modelName, ":cloud") || strings.HasSuffix(modelName, "-cloud")
}

// OllamaReachesCloud reports whether a request for this model is sent to
// api.ollama.com rather than to a daemon.
//
// This is the one definition of the routing rule, and every component that has
// to agree with it calls it: the runtime when it picks an endpoint, the compiler
// when it decides whether to emit the Secret reference, and credential
// compilation when it decides whether a gateway binding exists. Three
// independent copies of this predicate previously disagreed, which turned a
// valid configuration into either a denied egress call or a hard credential
// error.
//
// An explicit host normally wins: that is how an operator points at another
// machine, a container, or an authenticated proxy. The exception is a host that
// *is* ollama.com's endpoint — writing `host: api.ollama.com` is the cloud route
// spelled out longhand, and the runtime still expects a bearer token for it
// (NewOllamaModel takes the token from IsOllamaCloudEndpoint). Treating every
// explicit host as local left that configuration with no Secret reference and
// no credential binding, so the request went out unauthenticated and 401'd.
//
// A credential is required because api.ollama.com answers 401 before it looks at
// the model.
func OllamaReachesCloud(modelName, host string, hasCredential bool) bool {
	if host != "" && !IsOllamaCloudEndpoint(host) {
		return false
	}
	return hasCredential && isOllamaCloudModel(modelName)
}

// IsOllamaCloudEndpoint reports whether an endpoint is ollama.com's hosted API.
// Callers use it to tell a missing credential from an unreachable daemon.
//
// A bare "api.ollama.com" is accepted: url.Parse reads it as a path, not a host,
// so the scheme is added before matching, the same way a host:port is written.
func IsOllamaCloudEndpoint(host string) bool {
	u, err := url.Parse(withDefaultScheme(host))
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "api.ollama.com" || h == "ollama.com"
}

// withDefaultScheme makes a bare host an absolute URL so net/url reads it as one.
// The scheme check is case-insensitive; the host is returned unchanged when it
// already has one.
func withDefaultScheme(host string) string {
	lower := strings.ToLower(host)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return host
	}
	return "https://" + host
}

// resolveOllamaEndpoint picks the server a model should be sent to.
//
// The decision itself lives in OllamaReachesCloud so the compiler and credential
// compilation cannot disagree with the runtime about it. Everything this adds is
// the fallback: a cloud model with no key goes to the local daemon rather than
// api.ollama.com, because a signed-in daemon proxies cloud models and an
// unauthenticated api.ollama.com answers 401 before it looks at the model, so
// the alternative is not a different result but a guaranteed failure.
//
// The key is read in exactly one direction. Its absence keeps a cloud model
// local; its presence never promotes a local model, which would send a privately
// pulled name to an endpoint that does not have it.
func resolveOllamaEndpoint(config *OllamaConfig, host string) string {
	if host != "" {
		return host
	}
	if OllamaReachesCloud(config.Model, host, config.APIKey != "") {
		return ollamaCloudURL
	}
	return ollamaLocalURL
}

// OllamaModel implements model.LLM for Ollama models using the native Ollama SDK.
type OllamaModel struct {
	Config *OllamaConfig
	Client *api.Client
	Logger *slog.Logger
}

// Name returns the model name.
func (m *OllamaModel) Name() string {
	return m.Config.Model
}

// ollamaOptionKinds maps each option in api.Options to the type Ollama expects.
// It is built from the SDK, so it stays up to date. Ollama rejects the whole
// request when a known option has the wrong type, for example num_batch as a
// string. For pointer fields like main_gpu, the inner type is used.
var ollamaOptionKinds = func() map[string]reflect.Kind {
	kinds := make(map[string]reflect.Kind)
	for _, field := range reflect.VisibleFields(reflect.TypeFor[api.Options]()) {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		typ := field.Type
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.Kind() == reflect.Slice && typ.Elem().Kind() != reflect.String {
			continue
		}
		kinds[name] = typ.Kind()
	}
	return kinds
}()

// convertOllamaOptions converts the string values of ModelConfig Ollama
// options to the types Ollama expects.
func convertOllamaOptions(opts map[string]string) map[string]any {
	if opts == nil {
		return nil
	}
	converted := make(map[string]any, len(opts))
	for key, value := range opts {
		converted[key] = convertOllamaOption(ollamaOptionKinds[key], value)
	}
	return converted
}

// convertOllamaOption parses value as kind. Unknown options and values that do
// not parse are sent as they are. Ollama ignores unknown options and returns an
// error that names a wrong one.
//
// stop takes a JSON array of strings. Any other value is one stop sequence, so
// "[INST]" stays one item.
func convertOllamaOption(kind reflect.Kind, value string) any {
	switch kind {
	case reflect.Int:
		if v, err := strconv.Atoi(value); err == nil {
			return v
		}
	case reflect.Float32, reflect.Float64:
		if v, err := strconv.ParseFloat(value, 64); err == nil {
			return v
		}
	case reflect.Bool:
		if v, err := strconv.ParseBool(value); err == nil {
			return v
		}
	case reflect.Slice:
		var list []string
		if err := json.Unmarshal([]byte(value), &list); err == nil && list != nil {
			return list
		}
		return []string{value}
	}
	return value
}

// withOllamaAuthorization returns the transport config to build the client with.
//
// Only the cloud endpoint carries a credential, and the value here is the
// gateway placeholder rather than the Secret: Substrate replaces the header at
// egress. A local daemon keeps whatever the operator configured and never
// acquires an Authorization header from the key, so exporting OLLAMA_API_KEY for
// a cloud model cannot start authenticating local calls.
//
// The headers map is copied before the credential is set, so the caller's
// TransportConfig is not mutated and a stale Authorization header cannot
// outrank the placeholder.
func withOllamaAuthorization(tc TransportConfig, host, apiKey string) TransportConfig {
	if !IsOllamaCloudEndpoint(host) || apiKey == "" {
		return tc
	}
	headers := make(map[string]string, len(tc.Headers)+1)
	for name, value := range tc.Headers {
		if !strings.EqualFold(name, "Authorization") {
			headers[name] = value
		}
	}
	headers["Authorization"] = "Bearer " + apiKey
	tc.Headers = headers
	return tc
}

// NewOllamaModel creates a new Ollama model instance with a logger.
// It uses the native Ollama SDK client for full option support.
func NewOllamaModel(ctx context.Context, config *OllamaConfig) (*OllamaModel, error) {
	logger := logging.FromContext(ctx)
	host := config.Host
	if host == "" {
		host = env.OllamaAPIBase.Get()
	}
	host = resolveOllamaEndpoint(config, host)

	// Parse host URL
	baseURL, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid Ollama host URL %q: %w", host, err)
	}

	// Only the cloud carries a key; a local daemon must keep sending none.
	httpClient, err := BuildHTTPClient(withOllamaAuthorization(config.TransportConfig, host, config.APIKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Ollama HTTP client: %w", err)
	}

	// Create Ollama SDK client (NewClient takes *url.URL then *http.Client)
	client := api.NewClient(baseURL, httpClient)

	logger.InfoContext(ctx, "initialized Ollama model", "model", config.Model, "host", host)

	return &OllamaModel{
		Config: config,
		Client: client,
		Logger: logger,
	}, nil
}
