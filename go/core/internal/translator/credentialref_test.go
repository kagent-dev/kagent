package translator_test

import (
	"context"
	"slices"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/prototext"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The golden snapshot of an ActorTemplate is taken once and shared by every
// session, so a Harness credentialRef must compile to a gateway binding and a
// placeholder: the Secret value must not appear anywhere in the ActorTemplate.
func TestCompileAgentHarnessCredentialRefStaysOutOfActorTemplate(t *testing.T) {
	const value = "tickets-secret-value"
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tickets-auth", Namespace: "test"}, Data: map[string][]byte{"token": []byte(value)}}
	for _, runtime := range []string{"kagent", "byo"} {
		t.Run(runtime, func(t *testing.T) {
			harness := &v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Name: runtime, Namespace: "test"}, Spec: v1alpha3.HarnessSpec{
				Workload: v1alpha3.HarnessWorkload{Image: "example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				Env: []v1alpha3.RuntimeEnvVar{{Name: "TICKETS_API_TOKEN", CredentialRef: &v1alpha3.RuntimeCredentialRef{
					Name: secret.Name, Key: "token", URL: "https://tickets.example.com/api", Header: "Authorization", Prefix: "Bearer ",
				}}},
				Substrate: v1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: v1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}},
			}}
			template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Name: "helper", Namespace: "test"}, Spec: v1alpha3.AgentTemplateSpec{SystemPrompt: "help"}}
			if runtime == "kagent" {
				harness.Spec.Kagent = &v1alpha3.KagentHarness{}
				template.Spec.ModelConfig = &corev1.LocalObjectReference{Name: "default-model"}
			} else {
				harness.Spec.BYO = &v1alpha3.BYOHarness{}
				harness.Spec.Workload.Command = []string{"/agent"}
			}

			spec, err := compiler(t, modelConfig(), secret).CompileAgent(context.Background(), inlineAgent(harness, template))
			require.NoError(t, err)
			require.Contains(t, spec.Environment, corev1.EnvVar{Name: "TICKETS_API_TOKEN", Value: v2translator.CredentialPlaceholder})
			require.Contains(t, spec.Credentials, egress.Credential{Hostname: "tickets.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/test/tickets-auth/token"})
			require.True(t, slices.Contains(spec.EgressDestinations, "https://tickets.example.com:443"), "egress destinations = %v", spec.EgressDestinations)

			revisionID, err := spec.Digest()
			require.NoError(t, err)
			actorTemplate, err := substrate.ActorTemplateForRevision(&spec.Revision, revisionID)
			require.NoError(t, err)
			require.NotContains(t, prototext.Format(actorTemplate), value)
			require.NotContains(t, string(spec.Provenance), value)

			// Rotation is the gateway's job: a new Secret value must not
			// produce a new revision, ActorTemplate or golden snapshot.
			rotated := secret.DeepCopy()
			rotated.UID, rotated.Data["token"] = "rotated", []byte("rotated-value")
			next, err := compiler(t, modelConfig(), rotated).CompileAgent(context.Background(), inlineAgent(harness, template))
			require.NoError(t, err)
			nextID, err := next.Digest()
			require.NoError(t, err)
			require.Equal(t, revisionID, nextID)

			missingKey := secret.DeepCopy()
			missingKey.Data = map[string][]byte{"other": []byte("x")}
			_, err = compiler(t, modelConfig(), missingKey).CompileAgent(context.Background(), inlineAgent(harness, template))
			require.ErrorContains(t, err, `secret "tickets-auth" does not contain key "token"`)
			var validation *v2translator.ValidationError
			require.NotErrorAs(t, err, &validation, "a missing reference is a ResolvedRefs failure, not an incompatibility")
		})
	}
}
