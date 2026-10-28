package modeldiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	api "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

const (
	maxBody   = 2 << 20
	maxModels = 10000
	maxEntry  = 16 << 10
)

// Discovery errors are deliberately categorical: upstream bodies, URLs and
// credentials must never appear in logs, API errors or resource status.
var (
	errEndpoint     = errors.New("InvalidEndpoint")
	errResponse     = errors.New("InvalidResponse")
	errLimit        = errors.New("CatalogLimitExceeded")
	errRequest      = errors.New("RequestFailed")
	errUnauthorized = errors.New("ProviderUnauthorized")
	errUnsupported  = errors.New("UnsupportedProvider")
	errSecret       = errors.New(api.ModelProviderConfigReasonSecretUnresolved)
)

func endpointURL(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery {
		return "", errEndpoint
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	if u.RawPath != "" {
		u.RawPath = strings.TrimRight(u.RawPath, "/") + "/models"
	}
	return u.String(), nil
}

func newHTTPClient() *http.Client {
	return &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone(), Timeout: attemptTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func fetch(ctx context.Context, client *http.Client, endpoint, credential string) ([]*apiv1.DiscoveredModel, error) {
	target, err := endpointURL(endpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errEndpoint
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errRequest
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errRequest
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, errResponse
	}
	if len(body) > maxBody {
		return nil, errLimit
	}
	return parseModels(body)
}

func parseModels(body []byte) ([]*apiv1.DiscoveredModel, error) {
	var envelope struct {
		Object   string          `json:"object"`
		Data     json.RawMessage `json:"data"`
		HasMore  bool            `json:"has_more"`
		NextPage string          `json:"next_page"`
		Next     string          `json:"next"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Object != "list" || len(envelope.Data) == 0 || envelope.Data[0] != '[' || envelope.HasMore || envelope.NextPage != "" || envelope.Next != "" {
		return nil, errResponse
	}
	var models []*apiv1.DiscoveredModel
	if json.Unmarshal(envelope.Data, &models) != nil {
		return nil, errResponse
	}
	if len(models) > maxModels {
		return nil, errLimit
	}
	seen := make(map[string]bool, len(models))
	for _, m := range models {
		if m == nil || strings.TrimSpace(m.Id) == "" || m.Object != "model" || seen[m.Id] {
			return nil, errResponse
		}
		seen[m.Id] = true
		encoded, err := json.Marshal(m)
		if err != nil {
			return nil, errResponse
		}
		if len(encoded) > maxEntry {
			return nil, errLimit
		}
		slices.Sort(m.SupportedParameters)
		m.SupportedParameters = slices.Compact(m.SupportedParameters)
		if m.Architecture != nil {
			slices.Sort(m.Architecture.InputModalities)
			m.Architecture.InputModalities = slices.Compact(m.Architecture.InputModalities)
			slices.Sort(m.Architecture.OutputModalities)
			m.Architecture.OutputModalities = slices.Compact(m.Architecture.OutputModalities)
		}
	}
	slices.SortFunc(models, func(a, b *apiv1.DiscoveredModel) int { return strings.Compare(a.Id, b.Id) })
	if proto.Size(&apiv1.ListProviderModelsResponse{DiscoveredModels: models}) > maxBody-4096 {
		return nil, errLimit
	}
	return models, nil
}
