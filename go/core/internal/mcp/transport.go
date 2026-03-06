/*
Copyright 2025.

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

package mcp

import (
	"context"
	"net/http"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateMCPTransport creates an MCP transport from a RemoteMCPServer spec,
// resolving any headersFrom references using the kube client.
func CreateMCPTransport(ctx context.Context, kubeClient client.Client, server *v1alpha2.RemoteMCPServer) (mcpsdk.Transport, error) {
	headers, err := server.ResolveHeaders(ctx, kubeClient)
	if err != nil {
		return nil, err
	}

	httpClient := newHTTPClient(headers)

	switch server.Spec.Protocol {
	case v1alpha2.RemoteMCPServerProtocolSse:
		return &mcpsdk.SSEClientTransport{
			Endpoint:   server.Spec.URL,
			HTTPClient: httpClient,
		}, nil
	default:
		return &mcpsdk.StreamableClientTransport{
			Endpoint:   server.Spec.URL,
			HTTPClient: httpClient,
		}, nil
	}
}

// headerTransport is an http.RoundTripper that adds custom headers to requests.
type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if t.base == nil {
		t.base = http.DefaultTransport
	}
	return t.base.RoundTrip(req)
}

func newHTTPClient(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return http.DefaultClient
	}
	return &http.Client{
		Transport: &headerTransport{
			headers: headers,
			base:    http.DefaultTransport,
		},
	}
}
