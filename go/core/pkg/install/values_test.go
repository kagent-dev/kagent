package install

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestSubstrateInstallValues(t *testing.T) {
	for _, test := range []struct {
		namespace      string
		serviceAccount string
	}{
		{namespace: "kagent", serviceAccount: "kagent-controller"},
		{namespace: "team-a", serviceAccount: "custom-controller"},
	} {
		t.Run(test.namespace, func(t *testing.T) {
			var values struct {
				Preview []string `json:"preview"`
				AteAPI  struct {
					Authorization struct {
						BootstrapOwners []string `json:"bootstrapOwners"`
					} `json:"authorization"`
				} `json:"ateApi"`
				AtenetRouter struct {
					IngressAllowedSPIFFEIDs []string `json:"ingressAllowedSPIFFEIDs"`
				} `json:"atenetRouter"`
				CredentialProvider struct {
					NamespacePolicies []struct {
						Atespace          string   `json:"atespace"`
						AllowedNamespaces []string `json:"allowedNamespaces"`
					} `json:"namespacePolicies"`
				} `json:"credentialProvider"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(substrateInstallValues(test.namespace, test.serviceAccount)), &values))
			require.Contains(t, values.Preview, "PerTemplateSnapshotLocation")
			require.Equal(t, []string{
				"spiffe://cluster.local/ns/" + test.namespace + "/sa/" + test.serviceAccount,
				"system:serviceaccount:ate-system:ate-client",
			}, values.AteAPI.Authorization.BootstrapOwners)
			require.Equal(t, []string{
				"spiffe://cluster.local/ns/" + test.namespace + "/sa/" + test.serviceAccount,
			}, values.AtenetRouter.IngressAllowedSPIFFEIDs)
			require.Len(t, values.CredentialProvider.NamespacePolicies, 1)
			require.Equal(t, test.namespace, values.CredentialProvider.NamespacePolicies[0].Atespace)
			require.Equal(t, []string{test.namespace}, values.CredentialProvider.NamespacePolicies[0].AllowedNamespaces)
		})
	}
}
