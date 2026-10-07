package connection

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kubeconfigWithUser writes a kubeconfig whose current context authenticates
// with the given user block and points KUBECONFIG at it.
func kubeconfigWithUser(t *testing.T, user string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
current-context: lab
contexts:
- name: lab
  context:
    cluster: lab
    user: person
clusters:
- name: lab
  cluster:
    server: https://lab.example.test
users:
- name: person
  user:
`+user), 0o600))
	t.Setenv("KUBECONFIG", path)
}

// unexpiredJWT is a JWT whose only claim is an expiry far ahead; the oidc
// auth-provider hands an id-token out without a refresh while it is valid.
func unexpiredJWT() string {
	segment := func(json string) string { return base64.RawURLEncoding.EncodeToString([]byte(json)) }
	return segment(`{"alg":"none"}`) + "." + segment(`{"exp":4102444800}`) + ".signature"
}

func TestResolveCallerToken(t *testing.T) {
	idToken := unexpiredJWT()
	tests := []struct {
		name string
		flag string
		env  string
		user string
		want callerToken
	}{
		{
			name: "flag wins over every other source",
			flag: "flag-token",
			env:  "env-token",
			user: "    token: kubeconfig-token\n",
			want: callerToken{value: "flag-token", source: "--caller-token"},
		},
		{
			name: "environment wins over the kubeconfig",
			env:  "env-token",
			user: "    token: kubeconfig-token\n",
			want: callerToken{value: "env-token", source: "KAGENT_TOKEN"},
		},
		{
			name: "kubeconfig static token",
			user: "    token: kubeconfig-token\n",
			want: callerToken{value: "kubeconfig-token", source: "kubeconfig context lab"},
		},
		{
			name: "kubeconfig exec plugin",
			user: `    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: sh
      args:
      - -c
      - 'echo ''{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{"token":"exec-token"}}'''
      interactiveMode: Never
`,
			want: callerToken{value: "exec-token", source: "kubeconfig context lab"},
		},
		{
			name: "kubeconfig oidc id-token",
			user: `    auth-provider:
      name: oidc
      config:
        idp-issuer-url: https://issuer.example.test
        client-id: kubernetes
        id-token: ` + idToken + "\n",
			want: callerToken{value: idToken, source: "kubeconfig context lab"},
		},
		{
			name: "kubeconfig client certificate is no bearer",
			user: "    client-certificate-data: Y2VydA==\n    client-key-data: a2V5\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeconfigWithUser(t, tt.user)
			t.Setenv("KAGENT_TOKEN", tt.env)

			got, err := resolveCallerToken(tt.flag)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveCallerTokenWithoutKubeconfig(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("KAGENT_TOKEN", "")

	got, err := resolveCallerToken("")
	require.NoError(t, err)
	assert.Equal(t, callerToken{}, got)
	assert.False(t, got.isSet())
}

func TestResolveCallerTokenReportsAFailingExecPlugin(t *testing.T) {
	kubeconfigWithUser(t, `    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: sh
      args:
      - -c
      - 'exit 3'
      interactiveMode: Never
`)
	t.Setenv("KAGENT_TOKEN", "")

	_, err := resolveCallerToken("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubeconfig context lab")
}
