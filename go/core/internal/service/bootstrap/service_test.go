package bootstrap

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/protobuf/proto"

	contract "github.com/kagent-dev/kagent/go/api/bootstrap"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

type fixture struct {
	actor          *ateapipb.Actor
	template       *ateapipb.ActorTemplate
	binding        *database.BootstrapBinding
	bundle         *runtimev1.BootstrapServiceGetBootstrapConfigResponse
	changedBinding bool
	reads          int
}

func (f *fixture) GetActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if f.actor.Metadata.Atespace != atespace || f.actor.Metadata.Name != name {
		return nil, database.ErrNotFound
	}
	return f.actor, nil
}
func (f *fixture) GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error) {
	return f.template, nil
}
func (f *fixture) GetBootstrapBinding(_ context.Context, atespace, name string) (*database.BootstrapBinding, error) {
	if f.actor.ActorTemplate.Atespace != "team-a" || name != "template" || atespace != "team-a" {
		return nil, database.ErrNotFound
	}
	return f.binding, nil
}
func (f *fixture) GetBootstrapBundle(context.Context, database.BootstrapBinding) (*runtimev1.BootstrapServiceGetBootstrapConfigResponse, error) {
	f.reads++
	if f.changedBinding {
		return nil, database.ErrNotFound
	}
	return f.bundle, nil
}

func TestBootstrapAuthorization(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		change func(*fixture, jwt.MapClaims)
		want   serviceerrors.Code
	}{
		{name: "golden actor without Session"},
		{name: "runtime audience", change: func(_ *fixture, c jwt.MapClaims) { c["aud"] = []string{substrate.RuntimeJWTAudience} }, want: serviceerrors.CodeUnauthenticated},
		{name: "mixed runtime audience", change: func(_ *fixture, c jwt.MapClaims) {
			c["aud"] = []string{contract.Audience, substrate.RuntimeJWTAudience}
		}, want: serviceerrors.CodeUnauthenticated},
		{name: "replaced actor", change: func(f *fixture, _ jwt.MapClaims) { f.actor.Metadata.Uid = "replacement" }, want: serviceerrors.CodePermissionDenied},
		{name: "another template", change: func(f *fixture, _ jwt.MapClaims) { f.actor.ActorTemplate.Name = "another" }, want: serviceerrors.CodePermissionDenied},
		{name: "cross namespace", change: func(f *fixture, _ jwt.MapClaims) { f.actor.ActorTemplate.Atespace = "team-b" }, want: serviceerrors.CodePermissionDenied},
		{name: "initial binding pending", change: func(f *fixture, _ jwt.MapClaims) { f.binding.ActorTemplateUID = "" }, want: serviceerrors.CodeUnavailable},
		{name: "golden rebuild binding pending", change: func(f *fixture, _ jwt.MapClaims) { f.template.Metadata.Uid = "new-template" }, want: serviceerrors.CodeUnavailable},
		{name: "golden egress policy conflict", change: func(f *fixture, _ jwt.MapClaims) { f.template.GoldenEgressPolicy = &ateapipb.EgressPolicyTemplate{} }, want: serviceerrors.CodePermissionDenied},
		{name: "template spec conflict", change: func(f *fixture, _ jwt.MapClaims) { f.template.Containers[0].Image = "another-image" }, want: serviceerrors.CodePermissionDenied},
		{name: "binding deleted during read", change: func(f *fixture, _ jwt.MapClaims) { f.changedBinding = true }, want: serviceerrors.CodeUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			template := &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "template", Uid: "template-uid"}, Containers: []*ateapipb.Container{{Name: "runtime", Image: "agent:v1"}}}
			digest, err := substrate.ActorTemplateSpecDigest(template)
			require.NoError(t, err)
			bundle, err := contract.NewBundle(strings.Repeat("a", 64), []byte(`{}`), []byte(`{"name":"agent"}`))
			require.NoError(t, err)
			f := &fixture{template: template, bundle: bundle, binding: &database.BootstrapBinding{Revision: bundle.Revision, ActorTemplateUID: "template-uid", ActorTemplateSpecDigest: digest}, actor: &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "golden", Uid: "actor-uid"}, ActorTemplate: &ateapipb.ObjectRef{Atespace: "team-a", Name: "template"}}}
			claims := issuer.Claims("team-a", "golden", "actor-uid")
			claims["aud"] = []string{contract.Audience}
			if test.change != nil {
				test.change(f, claims)
			}
			service := NewService(verifier, f, f)
			ctx, err := service.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + issuer.Token(t, claims)}})
			var got *runtimev1.BootstrapServiceGetBootstrapConfigResponse
			if err == nil {
				got, err = service.Get(ctx)
			}
			require.Equal(t, test.want, serviceerrors.CodeOf(err), "%v", err)
			if test.want == "" {
				require.NoError(t, err)
				require.True(t, proto.Equal(bundle, got))
			} else {
				require.Nil(t, got)
			}
			if test.want == serviceerrors.CodeUnauthenticated || test.want == serviceerrors.CodePermissionDenied {
				require.Zero(t, f.reads)
			}
		})
	}
}
