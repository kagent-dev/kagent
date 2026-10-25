// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/http/httpguts"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// authenticatedMCPClient binds a connection and all of its background HTTP work
// to the initiating credential source. It never selects a later request's caller.
func (c *RuntimeMCPClient) authenticatedMCPClient(ctx context.Context, server *v1alpha3.RemoteMCPServer) (*http.Client, error) {
	result := &http.Client{Transport: mcpTracedTransport()}
	configuration := server.Spec.Headers
	if len(configuration) == 0 {
		return result, nil
	}
	if len(configuration) > 16 {
		return nil, fmt.Errorf("credential headers require 1 to 16 headers")
	}
	names := map[string]bool{}
	for _, header := range configuration {
		name := strings.ToLower(header.Name)
		if names[name] || !egress.ValidCredentialHeader(header.Name) {
			return nil, fmt.Errorf("invalid or duplicate credential header")
		}
		names[name] = true
		if (header.ValueFrom.SecretKeyRef == nil) == (header.ValueFrom.TokenPassthrough == nil) {
			return nil, fmt.Errorf("credential headers require exactly one source per header")
		}
	}
	endpoint, err := url.Parse(server.Spec.URL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return nil, fmt.Errorf("invalid authenticated MCP endpoint")
	}
	session, _ := auth.AuthSessionFrom(ctx)
	source, _ := session.(auth.TokenSource)
	result.Transport = &mcpAuthenticationTransport{base: result.Transport, endpoint: endpoint, client: c.kubeClient, namespace: server.Namespace, headers: server.Spec.DeepCopy().Headers, source: source}
	return result, nil
}

type mcpAuthenticationTransport struct {
	base      http.RoundTripper
	endpoint  *url.URL
	client    client.Client
	namespace string
	headers   []v1alpha3.Header
	source    auth.TokenSource
}

func (t *mcpAuthenticationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != t.endpoint.Scheme || !strings.EqualFold(request.URL.Host, t.endpoint.Host) {
		return nil, fmt.Errorf("authenticated MCP request changed destination")
	}
	request = request.Clone(request.Context())
	var token string
	for _, header := range t.headers {
		var value string
		if ref := header.ValueFrom.SecretKeyRef; ref != nil {
			secret := &corev1.Secret{}
			if err := t.client.Get(request.Context(), client.ObjectKey{Namespace: t.namespace, Name: ref.Name}, secret); err != nil {
				return nil, fmt.Errorf("MCP authentication Secret is unavailable")
			}
			data, found := secret.Data[ref.Key]
			if !found || len(data) == 0 {
				return nil, fmt.Errorf("MCP authentication Secret key is unavailable")
			}
			value = string(data)
		} else {
			if t.source == nil {
				return nil, fmt.Errorf("MCP invocation credential is unavailable")
			}
			if token == "" {
				var err error
				token, err = t.source.Token(request.Context())
				if err != nil || token == "" {
					return nil, fmt.Errorf("MCP invocation credential is unavailable or expired")
				}
			}
			value = token
		}
		if header.Prefix != nil {
			value = *header.Prefix + value
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return nil, fmt.Errorf("MCP credential cannot be represented as an HTTP header")
		}
		request.Header.Set(header.Name, value)
	}
	return t.base.RoundTrip(request)
}
