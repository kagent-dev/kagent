package v1alpha3

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func testMCPCredentialHeaderValidation(t *testing.T, cl ctrlclient.Client, namespace string) {
	t.Helper()
	for _, tc := range []struct {
		name   string
		header string
		reject string
	}{
		{name: "authorization", header: "Authorization"},
		{name: "api-key", header: "X-API-Key"},
		{name: "token-characters", header: "X-!#$%&'*+-.^_`|~0123456789"},
		{name: "max-length", header: strings.Repeat("x", 256)},
		{name: "empty", reject: "valid HTTP header name"},
		{name: "too-long", header: strings.Repeat("x", 257), reject: "may not be more than 256"},
		{name: "whitespace", header: "X Key", reject: "valid HTTP header name"},
		{name: "newline", header: "X-Key\r\nInjected", reject: "valid HTTP header name"},
		{name: "colon", header: "X:Key", reject: "valid HTTP header name"},
		{name: "backslash", header: "X\\Key", reject: "valid HTTP header name"},
		{name: "unicode", header: "X-名", reject: "valid HTTP header name"},
		{name: "host", header: "Host", reject: "cannot carry an egress credential"},
		{name: "connection", header: "cOnNeCtIoN", reject: "cannot carry an egress credential"},
		{name: "content-length", header: "Content-Length", reject: "cannot carry an egress credential"},
		{name: "transfer-encoding", header: "Transfer-Encoding", reject: "cannot carry an egress credential"},
		{name: "te", header: "TE", reject: "cannot carry an egress credential"},
		{name: "trailer", header: "Trailer", reject: "cannot carry an egress credential"},
		{name: "upgrade", header: "Upgrade", reject: "cannot carry an egress credential"},
		{name: "keep-alive", header: "Keep-Alive", reject: "cannot carry an egress credential"},
		{name: "proxy-authorization", header: "Proxy-Authorization", reject: "cannot carry an egress credential"},
		{name: "proxy-authenticate", header: "Proxy-Authenticate", reject: "cannot carry an egress credential"},
		{name: "cookie", header: "Cookie", reject: "cannot carry an egress credential"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &RemoteMCPServer{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "mcp-header-" + tc.name},
				Spec: RemoteMCPServerSpec{
					Description: "MCP credential header validation",
					URL:         "https://mcp.example.com/mcp",
					HeadersFrom: []ValueRef{{Name: tc.header, ValueFrom: &ValueSource{Type: SecretValueSource, Name: "auth", Key: "token"}}},
				},
			}
			err := cl.Create(t.Context(), server)
			if tc.reject != "" {
				require.ErrorContains(t, err, tc.reject)
				require.ErrorContains(t, err, "spec.headersFrom[0].name")
				return
			}
			require.NoError(t, err)
			require.NoError(t, cl.Get(t.Context(), ctrlclient.ObjectKeyFromObject(server), server))
			require.Equal(t, tc.header, server.Spec.HeadersFrom[0].Name)
			server.Spec.HeadersFrom[0].Name = "Cookie"
			require.ErrorContains(t, cl.Update(t.Context(), server), "cannot carry an egress credential")
		})
	}
	for _, source := range []ValueRef{
		{Name: "Cookie", Value: "session=example"},
		{Name: "Cookie", ValueFrom: &ValueSource{Type: ConfigMapValueSource, Name: "headers", Key: "cookie"}},
	} {
		server := &RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, GenerateName: "mcp-ordinary-header-"},
			Spec:       RemoteMCPServerSpec{Description: "Ordinary headers are not gateway credentials", URL: "https://mcp.example.com/mcp", HeadersFrom: []ValueRef{source}},
		}
		require.NoError(t, cl.Create(t.Context(), server))
		server.Spec.HeadersFrom[0] = ValueRef{Name: "Cookie", ValueFrom: &ValueSource{Type: SecretValueSource, Name: "auth", Key: "token"}}
		require.ErrorContains(t, cl.Update(t.Context(), server), "cannot carry an egress credential")
	}
	t.Run("header count", func(t *testing.T) {
		headers := make([]ValueRef, 64)
		for i := range headers {
			headers[i] = ValueRef{Name: fmt.Sprintf("X-Header-%d", i), Value: "example"}
		}
		server := &RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "mcp-header-count"},
			Spec:       RemoteMCPServerSpec{Description: "Header count validation", URL: "https://mcp.example.com/mcp", HeadersFrom: headers},
		}
		require.NoError(t, cl.Create(t.Context(), server))
		server.Spec.HeadersFrom = append(server.Spec.HeadersFrom, ValueRef{Name: "X-Extra", Value: "example"})
		require.ErrorContains(t, cl.Update(t.Context(), server), "must have at most 64 items")
	})
}
