package v1alpha3

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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
		{name: "empty", reject: "should match"},
		{name: "too-long", header: strings.Repeat("x", 257), reject: "may not be more than 256"},
		{name: "whitespace", header: "X Key", reject: "should match"},
		{name: "newline", header: "X-Key\r\nInjected", reject: "should match"},
		{name: "colon", header: "X:Key", reject: "should match"},
		{name: "backslash", header: "X\\Key", reject: "should match"},
		{name: "unicode", header: "X-名", reject: "should match"},
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
					URL:         "https://mcp.example.com/mcp", Headers: []Header{{Name: tc.header, ValueFrom: HeaderValueSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "token"}}}},
				},
			}
			err := cl.Create(t.Context(), server)
			if tc.reject != "" {
				require.ErrorContains(t, err, tc.reject)
				require.ErrorContains(t, err, "spec.headers[0].name")
				return
			}
			require.NoError(t, err)
			require.NoError(t, cl.Get(t.Context(), ctrlclient.ObjectKeyFromObject(server), server))
			require.Equal(t, tc.header, server.Spec.Headers[0].Name)
			server.Spec.Headers[0].Name = "Cookie"
			require.ErrorContains(t, cl.Update(t.Context(), server), "cannot carry an egress credential")
		})
	}
}
