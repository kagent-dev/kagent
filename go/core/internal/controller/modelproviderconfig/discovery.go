/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package modelproviderconfig discovers models from OpenAI-compatible endpoints
// and publishes their availability in ModelProviderConfig status.
package modelproviderconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/model"
)

const (
	discoveryTimeout = 15 * time.Second
	maxResponseBytes = 2 << 20
)

// Discoverer lists the models served by a ModelProviderConfig endpoint.
type Discoverer struct {
	client client.Client //nolint:forbidigo // TODO(#3235): migrate to apiclient.Client
	http   *http.Client
}

func NewDiscoverer(kubeClient client.Client) *Discoverer { //nolint:forbidigo // TODO(#3235): migrate to apiclient.Client
	return &Discoverer{client: kubeClient, http: &http.Client{
		// The default transport is deliberate: it honours the process trust store and SSL_CERT_FILE, so private-CA endpoints work.
		Timeout: discoveryTimeout,
		// A redirect could carry the discovery token to another scheme or port, or reach a host the config never named.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var _ model.ProviderModelRefresher = (*Discoverer)(nil)

// RefreshModelProviderConfigModels implements model.ProviderModelRefresher.
func (d *Discoverer) RefreshModelProviderConfigModels(ctx context.Context, namespace, name string) ([]string, error) {
	config := &v1alpha3.ModelProviderConfig{}
	if err := d.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, config); err != nil {
		return nil, err
	}
	return d.discover(ctx, config)
}

// discover returns the sorted model IDs listed by GET <endpoint>/models.
func (d *Discoverer) discover(ctx context.Context, config *v1alpha3.ModelProviderConfig) ([]string, error) {
	url := strings.TrimRight(config.Spec.GetEndpoint(), "/") + "/models"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build discovery request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if auth := config.Spec.DiscoveryAuth; auth != nil {
		token, err := d.token(ctx, config.Namespace, auth)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := d.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request models: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("model endpoint returned HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read models response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("models response exceeds %d bytes", maxResponseBytes)
	}
	return parseModelIDs(body)
}

func (d *Discoverer) token(ctx context.Context, namespace string, auth *v1alpha3.ModelDiscoveryAuth) (string, error) {
	ref := auth.SecretRef
	secret := &corev1.Secret{}
	if err := d.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, secret); err != nil {
		return "", fmt.Errorf("get discovery Secret %q: %w", ref.Name, err)
	}
	value := strings.TrimSpace(string(secret.Data[ref.Key]))
	if value == "" {
		return "", fmt.Errorf("discovery Secret %q has no value for key %q", ref.Name, ref.Key)
	}
	return value, nil
}

type modelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// parseModelIDs extracts the sorted, de-duplicated, non-empty IDs from an OpenAI-compatible model list.
func parseModelIDs(body []byte) ([]string, error) {
	var list modelList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("parse models response: %w", err)
	}
	if list.Data == nil {
		return nil, errors.New("models response has no data array")
	}
	ids := make([]string, 0, len(list.Data))
	for _, entry := range list.Data {
		if entry.ID != "" {
			ids = append(ids, entry.ID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}
