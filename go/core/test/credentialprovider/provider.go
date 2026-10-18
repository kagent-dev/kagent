// Package credentialprovider is the e2e suite's credential provider: the
// egress gateway resolves a Session's provider credential URIs through it
// (Substrate's credentialProvider.additionalProviders), as it resolves them
// through the installation's own provider, which finds the Session's person
// from the Actor it is asked for. It answers every Actor with a token naming
// the Actor, so a test reads from the credential an upstream received which
// Session sent the request, and refuses the provider instances it is told to,
// as a provider refuses a person who has not connected the instance.
package credentialprovider

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/credproviderpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TokenPrefix starts every token the provider answers; the Actor's name
// follows it.
const TokenPrefix = "e2e-person-"

// Token is the token the provider answers for the Actor named actor.
func Token(actor string) string { return TokenPrefix + actor }

// GitUser is the user of the Basic credential the provider answers for git.
const GitUser = "x-access-token"

// Provider answers the credential URIs of Authority, ate-secret://<Authority>/<instance>/<git|api>,
// except those of a Refused instance.
type Provider struct {
	credproviderpb.UnimplementedCredentialProviderServer
	Authority string
	Refused   []string
}

// Answer is the credential the provider gives the Actor of actorSPIFFEID
// (spiffe://<trust domain>/actor/<atespace>/<actor>) for uri: for git the
// Basic credential already encoded, for the API the raw token, each naming the
// Actor (Token). A refused instance is PermissionDenied, any other URI
// InvalidArgument.
func (p *Provider) Answer(uri, actorSPIFFEID string) ([]byte, error) {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "ate-secret" || parsed.Host != p.Authority {
		return nil, status.Errorf(codes.InvalidArgument, "e2e credential provider: %q is not a credential URI of %s", uri, p.Authority)
	}
	instance, kind, ok := strings.Cut(strings.TrimPrefix(parsed.Path, "/"), "/")
	if !ok || instance == "" || (kind != "git" && kind != "api") {
		return nil, status.Errorf(codes.InvalidArgument, "e2e credential provider: %q names no provider instance and git or api", uri)
	}
	actor, err := url.Parse(actorSPIFFEID)
	if err != nil || actor.Scheme != "spiffe" || !strings.HasPrefix(actor.Path, "/actor/") {
		return nil, status.Errorf(codes.InvalidArgument, "e2e credential provider: %q is not an Actor's SPIFFE ID", actorSPIFFEID)
	}
	if slices.Contains(p.Refused, instance) {
		return nil, status.Errorf(codes.PermissionDenied, "e2e credential provider: the person of %s has not connected %s", path.Base(actor.Path), instance)
	}
	token := Token(path.Base(actor.Path))
	if kind == "git" {
		return []byte(base64.StdEncoding.EncodeToString([]byte(GitUser + ":" + token))), nil
	}
	return []byte(token), nil
}

// FetchSecret answers the egress gateway.
func (p *Provider) FetchSecret(_ context.Context, request *credproviderpb.FetchSecretRequest) (*credproviderpb.FetchSecretResponse, error) {
	answer, err := p.Answer(request.GetUri(), request.GetActorSpiffeId())
	if err != nil {
		return nil, err
	}
	return &credproviderpb.FetchSecretResponse{OpaqueBytes: answer}, nil
}

// ActorOf returns the Actor a credential the provider answered names, from
// the Authorization header an upstream received: "Basic" with the git
// credential or "Bearer" with the token.
func ActorOf(authorization string) (string, error) {
	scheme, credential, ok := strings.Cut(authorization, " ")
	if !ok {
		return "", fmt.Errorf("authorization %q has no scheme", authorization)
	}
	token := credential
	switch scheme {
	case "Basic":
		decoded, err := base64.StdEncoding.DecodeString(credential)
		if err != nil {
			return "", fmt.Errorf("basic credential: %w", err)
		}
		user, password, found := strings.Cut(string(decoded), ":")
		if !found || user != GitUser {
			return "", fmt.Errorf("basic credential is not the provider's git credential")
		}
		token = password
	case "Bearer":
	default:
		return "", fmt.Errorf("authorization scheme %q is not the provider's", scheme)
	}
	actor, found := strings.CutPrefix(token, TokenPrefix)
	if !found || actor == "" {
		return "", fmt.Errorf("the credential is not a token of the provider")
	}
	return actor, nil
}
