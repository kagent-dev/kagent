package grpcserver

import (
	"context"
	"errors"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"google.golang.org/grpc/codes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// scopeUnavailableAuthorizer allows every action but fails to scope one verb,
// which is all a capability load needs to fail.
type scopeUnavailableAuthorizer struct{ verb auth.Verb }

func (scopeUnavailableAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return nil
}

func (a scopeUnavailableAuthorizer) Scope(_ context.Context, _ auth.Principal, verb auth.Verb, _ string) (auth.AuthorizationScope, error) {
	if verb == a.verb {
		return auth.AuthorizationScope{}, errors.New("scope unavailable")
	}
	return auth.AuthorizationScope{Kind: auth.ScopeAll}, nil
}

// A failed capability lookup must not report failure after the store changed;
// the caller cannot tell that response from one that wrote nothing.
func TestMutationsDoNotWriteWhenCapabilitiesFail(t *testing.T) {
	authorizer := scopeUnavailableAuthorizer{verb: auth.VerbDelete}

	t.Run("create", func(t *testing.T) {
		connection, store := newScopedConnection(t, authorizer)
		_, err := apiv1alpha1.NewAgentTemplateServiceClient(connection).CreateAgentTemplate(
			t.Context(), &apiv1alpha1.CreateAgentTemplateRequest{
				Ref:      &apiv1alpha1.ResourceReference{Namespace: "team", Name: "created"},
				Resource: structured(t, testAgentTemplate("team", "created", "gpt"), agentTemplateKind),
			})
		assertCode(t, err, codes.PermissionDenied)

		err = store.Get(t.Context(), types.NamespacedName{Namespace: "team", Name: "created"}, &v1alpha3.AgentTemplate{})
		if !apierrors.IsNotFound(err) {
			t.Fatalf("CreateAgentTemplate() reported failure but stored the template, Get() error = %v", err)
		}
	})

	t.Run("update", func(t *testing.T) {
		connection, store := newScopedConnection(t, authorizer, testAgentTemplate("team", "existing", "gpt"))
		_, err := apiv1alpha1.NewAgentTemplateServiceClient(connection).UpdateAgentTemplate(
			t.Context(), &apiv1alpha1.UpdateAgentTemplateRequest{
				Ref:      &apiv1alpha1.ResourceReference{Namespace: "team", Name: "existing"},
				Resource: structured(t, testAgentTemplate("team", "existing", "claude"), agentTemplateKind),
			})
		assertCode(t, err, codes.PermissionDenied)

		stored := &v1alpha3.AgentTemplate{}
		if err := store.Get(t.Context(), types.NamespacedName{Namespace: "team", Name: "existing"}, stored); err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if stored.Spec.ModelConfig.Name != "gpt" {
			t.Fatalf("UpdateAgentTemplate() reported failure but stored model config %q", stored.Spec.ModelConfig.Name)
		}
	})
}
