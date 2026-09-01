package adkconfig

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRenderBedrockCredentialsFromReferences(t *testing.T) {
	secret := types.NamespacedName{Namespace: "test", Name: "credentials"}
	tests := []struct {
		name       string
		references []v2translator.ModelConfigReference
		want       []string
	}{
		{
			name:       "bearer",
			references: []v2translator.ModelConfigReference{{NamespacedName: secret, Kind: "Secret", Key: env.AWSBearerTokenBedrock.Name()}},
			want:       []string{env.AWSRegion.Name(), env.AWSBearerTokenBedrock.Name()},
		},
		{
			name: "IAM with session token",
			references: []v2translator.ModelConfigReference{
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSAccessKeyID.Name()},
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSSecretAccessKey.Name()},
				{NamespacedName: secret, Kind: "Secret", Key: env.AWSSessionToken.Name()},
			},
			want: []string{env.AWSRegion.Name(), env.AWSAccessKeyID.Name(), env.AWSSecretAccessKey.Name(), env.AWSSessionToken.Name()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &v2translator.ResolvedModelConfig{
				Config: &v1alpha3.ModelConfig{
					ObjectMeta: metav1.ObjectMeta{Namespace: secret.Namespace},
					Spec: v1alpha3.ModelConfigSpec{
						Provider: v1alpha3.ModelProviderBedrock, Model: "claude", APIKeySecret: secret.Name,
						Bedrock: &v1alpha3.BedrockConfig{Region: "us-east-1"},
					},
				},
				References: tt.references,
			}
			_, data, err := (&Builder{}).translateModel(context.Background(), resolved)
			require.NoError(t, err)
			names := make([]string, 0, len(data.EnvVars))
			for _, variable := range data.EnvVars {
				names = append(names, variable.Name)
			}
			require.ElementsMatch(t, tt.want, names)
		})
	}
}
