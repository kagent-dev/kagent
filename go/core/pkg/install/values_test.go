package install

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestSubstrateInstallValues(t *testing.T) {
	for _, namespace := range []string{"kagent", "team-a"} {
		t.Run(namespace, func(t *testing.T) {
			var values struct {
				Preview []string `json:"preview"`
				AteAPI  struct {
					Authorization struct {
						BootstrapOwners []string `json:"bootstrapOwners"`
					} `json:"authorization"`
				} `json:"ateApi"`
				CredentialProvider struct {
					NamespacePolicies []struct {
						Atespace          string   `json:"atespace"`
						AllowedNamespaces []string `json:"allowedNamespaces"`
					} `json:"namespacePolicies"`
				} `json:"credentialProvider"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(substrateInstallValues(namespace, "kagent-controller")), &values))
			require.Contains(t, values.Preview, "PerTemplateSnapshotLocation")
			require.Equal(t, []string{
				"spiffe://cluster.local/ns/" + namespace + "/sa/kagent-controller",
				"system:serviceaccount:ate-system:ate-client",
			}, values.AteAPI.Authorization.BootstrapOwners)
			require.Len(t, values.CredentialProvider.NamespacePolicies, 1)
			require.Equal(t, namespace, values.CredentialProvider.NamespacePolicies[0].Atespace)
			require.Equal(t, []string{namespace}, values.CredentialProvider.NamespacePolicies[0].AllowedNamespaces)
		})
	}
}
