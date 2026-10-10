package jwtadmission

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/test/grant"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const (
	issuer   = "https://workspaces.example.test"
	audience = "kagent"
	person   = "person@example.test"
)

func workspaceGrant() grant.Grant {
	return grant.Grant{
		Volume:    grant.Volume{Driver: "nfs.csi.k8s.io", Handle: "nfs.example.test#exports#workspace-1##"},
		Mounts:    []grant.Mount{{SubPath: "sessions/${SESSION_ID}"}, {SubPath: "mirrors", ReadOnly: true}},
		Changed:   []string{"acme/api", "acme/web"},
		Providers: []grant.Provider{{Hostname: "github.com", Audience: "https://github.com", Scheme: "Basic", Username: "x-access-token", Broker: "workspaces"}},
	}
}

func workspaceSource() *apiv1alpha1.SessionVolumeSource {
	return &apiv1alpha1.SessionVolumeSource{
		Volume: &apiv1alpha1.SessionVolume{CsiDriver: "nfs.csi.k8s.io", VolumeHandle: "nfs.example.test#exports#workspace-1##"},
		Mounts: []*apiv1alpha1.SessionVolumeMount{
			{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace"},
			{SubPath: "mirrors", MountPath: "/mirrors", ReadOnly: true},
		},
	}
}

// issuerKeys publishes the JWKS of the signers it currently holds, so a test
// can rotate the issuer's keys or take it offline.
type issuerKeys struct {
	mu      sync.Mutex
	signers []*grant.Signer
	down    bool
	fetches int
}

func (i *issuerKeys) publish(signers ...*grant.Signer) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.signers, i.down = signers, false
}

func (i *issuerKeys) takeDown() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.down = true
}

func (i *issuerKeys) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.fetches++
	if i.down {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	var keys grant.JWKS
	for _, signer := range i.signers {
		keys.Keys = append(keys.Keys, signer.JWKS().Keys...)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(keys)
}

// clock is the admission's time, moved by the test.
type clock struct{ now time.Time }

func (c *clock) advance(by time.Duration) { c.now = c.now.Add(by) }

func newTestAdmission(t *testing.T, published ...*grant.Signer) (*Admission, *issuerKeys, *clock) {
	t.Helper()
	keys := &issuerKeys{}
	keys.publish(published...)
	server := httptest.NewServer(keys)
	t.Cleanup(server.Close)
	admission, err := New(Config{JWKSURL: server.URL + grant.JWKSPath, Issuer: issuer, Audience: audience}, server.Client())
	require.NoError(t, err)
	at := &clock{now: time.Now()}
	admission.keys.now = func() time.Time { return at.now }
	return admission, keys, at
}

func newSigner(t *testing.T, issuer, audience string) *grant.Signer {
	t.Helper()
	signer, err := grant.NewSigner(issuer, audience)
	require.NoError(t, err)
	return signer
}

func sign(t *testing.T, signer *grant.Signer, subject string, workspace grant.Grant, ttl time.Duration) string {
	t.Helper()
	token, err := signer.SignGrant(subject, workspace, ttl)
	require.NoError(t, err)
	return token
}

func TestAdmitsAValidGrant(t *testing.T) {
	signer := newSigner(t, issuer, audience)
	admission, _, _ := newTestAdmission(t, signer)

	admitted, err := admission.Admit(t.Context(), person, workspaceSource(), sign(t, signer, person, workspaceGrant(), time.Minute))
	require.NoError(t, err)
	require.Equal(t, []string{"acme/api", "acme/web"}, admitted.Changed)
	require.Len(t, admitted.Providers, 1)
	require.True(t, proto.Equal(&apiv1alpha1.SessionProvider{
		Hostname: "github.com", Audience: "https://github.com", Scheme: "Basic", Username: "x-access-token", Broker: "workspaces",
	}, admitted.Providers[0]), "provider = %v", admitted.Providers[0])

	// A grant may admit fewer mounts than it allows, and a directory it allows
	// read-write also read-only.
	source := workspaceSource()
	source.Mounts = []*apiv1alpha1.SessionVolumeMount{{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace", ReadOnly: true}}
	_, err = admission.Admit(t.Context(), person, source, sign(t, signer, person, workspaceGrant(), time.Minute))
	require.NoError(t, err)
}

func TestRefusesWithTheNamedReason(t *testing.T) {
	signer := newSigner(t, issuer, audience)
	otherAudience := newSigner(t, issuer, "another-kagent")
	otherIssuer := newSigner(t, "https://elsewhere.example.test", audience)
	unpublished := newSigner(t, issuer, audience)
	admission, _, _ := newTestAdmission(t, signer, otherAudience, otherIssuer)
	valid := sign(t, signer, person, workspaceGrant(), time.Minute)

	for _, test := range []struct {
		name   string
		token  string
		caller string
		source func(*apiv1alpha1.SessionVolumeSource)
		code   serviceerrors.Code
		reason string
	}{
		{name: "no grant", token: "", code: serviceerrors.CodePermissionDenied, reason: "no admission grant"},
		{name: "expired", token: sign(t, signer, person, workspaceGrant(), -time.Minute), code: serviceerrors.CodePermissionDenied, reason: "admission grant expired"},
		{name: "wrong sub", token: valid, caller: "someone-else@example.test", code: serviceerrors.CodePermissionDenied, reason: "admission grant names another person"},
		{name: "wrong volume handle", token: valid, source: func(s *apiv1alpha1.SessionVolumeSource) {
			s.Volume.VolumeHandle = "nfs.example.test#exports#workspace-2##"
		}, code: serviceerrors.CodePermissionDenied, reason: "admission grant names another volume"},
		{name: "wrong volume driver", token: valid, source: func(s *apiv1alpha1.SessionVolumeSource) { s.Volume.CsiDriver = "efs.csi.aws.com" }, code: serviceerrors.CodePermissionDenied, reason: "admission grant names another volume"},
		{name: "a directory the grant does not allow", token: valid, source: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[1].SubPath = "sessions" }, code: serviceerrors.CodePermissionDenied, reason: `admission grant does not allow the read-only mount of "sessions"`},
		{name: "another session's directory", token: valid, source: func(s *apiv1alpha1.SessionVolumeSource) {
			s.Mounts[0].SubPath = "sessions/0199c6b8-0000-7000-8000-000000000000"
		}, code: serviceerrors.CodePermissionDenied, reason: "admission grant does not allow the read-write mount"},
		{name: "the mirrors read-write", token: valid, source: func(s *apiv1alpha1.SessionVolumeSource) {
			s.Mounts = []*apiv1alpha1.SessionVolumeMount{{SubPath: "mirrors", MountPath: "/mirrors"}}
		}, code: serviceerrors.CodePermissionDenied, reason: `admission grant does not allow the read-write mount of "mirrors"`},
		{name: "wrong aud", token: sign(t, otherAudience, person, workspaceGrant(), time.Minute), code: serviceerrors.CodePermissionDenied, reason: "admission grant is for another audience"},
		{name: "wrong iss", token: sign(t, otherIssuer, person, workspaceGrant(), time.Minute), code: serviceerrors.CodePermissionDenied, reason: "admission grant is from an unknown issuer"},
		{name: "unknown kid", token: sign(t, unpublished, person, workspaceGrant(), time.Minute), code: serviceerrors.CodePermissionDenied, reason: "admission grant is signed by an unknown key"},
		{name: "tampered payload", token: tamper(t, valid), code: serviceerrors.CodePermissionDenied, reason: "admission grant signature is invalid"},
		{name: "not a JWT", token: "not-a-jwt", code: serviceerrors.CodePermissionDenied, reason: "admission grant is malformed"},
		{name: "a provider without a broker", token: sign(t, signer, person, func() grant.Grant {
			workspace := workspaceGrant()
			workspace.Providers[0].Broker = ""
			return workspace
		}(), time.Minute), code: serviceerrors.CodePermissionDenied, reason: "admission grant is malformed: a provider"},
		{name: "a changed path leaving the working directory", token: sign(t, signer, person, func() grant.Grant {
			workspace := workspaceGrant()
			workspace.Changed = []string{"../etc"}
			return workspace
		}(), time.Minute), code: serviceerrors.CodePermissionDenied, reason: "admission grant is malformed: a changed repository"},
	} {
		t.Run(test.name, func(t *testing.T) {
			caller := person
			if test.caller != "" {
				caller = test.caller
			}
			source := workspaceSource()
			if test.source != nil {
				test.source(source)
			}
			admitted, err := admission.Admit(t.Context(), caller, source, test.token)
			require.True(t, serviceerrors.IsCode(err, test.code), "Admit() error = %v, want code %s", err, test.code)
			require.ErrorContains(t, err, test.reason)
			require.Equal(t, session.Admitted{}, admitted)
			if test.token != "" {
				require.NotContains(t, err.Error(), test.token, "a refusal never carries the grant")
			}
		})
	}
}

func TestRefusesATokenWithoutAVolumeSource(t *testing.T) {
	signer := newSigner(t, issuer, audience)
	admission, keys, _ := newTestAdmission(t, signer)
	_, err := admission.Admit(t.Context(), person, nil, sign(t, signer, person, workspaceGrant(), time.Minute))
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), "Admit() error = %v", err)
	require.Zero(t, keys.fetches, "nothing to admit needs no key")
}

// A rotated JWKS is read on the first grant with the new key id, by the same
// admission: no controller restart. The withdrawn key admits nothing after.
func TestAcceptsARotatedKeyWithoutARestart(t *testing.T) {
	before := newSigner(t, issuer, audience)
	admission, keys, at := newTestAdmission(t, before)
	_, err := admission.Admit(t.Context(), person, workspaceSource(), sign(t, before, person, workspaceGrant(), time.Minute))
	require.NoError(t, err)

	after := newSigner(t, issuer, audience)
	keys.publish(after)
	at.advance(refreshInterval)
	_, err = admission.Admit(t.Context(), person, workspaceSource(), sign(t, after, person, workspaceGrant(), time.Minute))
	require.NoError(t, err, "the rotated key admits")
	require.Equal(t, 2, keys.fetches)

	_, err = admission.Admit(t.Context(), person, workspaceSource(), sign(t, before, person, workspaceGrant(), time.Minute))
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), "Admit() error = %v", err)
	require.ErrorContains(t, err, "unknown key")
}

// Unknown key ids refetch the JWKS at most once per refreshInterval.
func TestUnknownKeysDoNotHammerTheIssuer(t *testing.T) {
	signer := newSigner(t, issuer, audience)
	admission, keys, _ := newTestAdmission(t, signer)
	for range 5 {
		_, err := admission.Admit(t.Context(), person, workspaceSource(), sign(t, newSigner(t, issuer, audience), person, workspaceGrant(), time.Minute))
		require.ErrorContains(t, err, "unknown key")
	}
	require.Equal(t, 1, keys.fetches)
}

func TestAnUnreachableJWKSIsUnavailable(t *testing.T) {
	t.Run("never fetched", func(t *testing.T) {
		signer := newSigner(t, issuer, audience)
		admission, keys, _ := newTestAdmission(t, signer)
		keys.takeDown()
		_, err := admission.Admit(t.Context(), person, workspaceSource(), sign(t, signer, person, workspaceGrant(), time.Minute))
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnavailable), "Admit() error = %v, want Unavailable", err)
		require.False(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
		require.ErrorContains(t, err, "admission grant keys are unavailable")
	})
	t.Run("no server", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		server.Close()
		admission, err := New(Config{JWKSURL: server.URL + grant.JWKSPath, Issuer: issuer, Audience: audience}, nil)
		require.NoError(t, err)
		signer := newSigner(t, issuer, audience)
		_, err = admission.Admit(t.Context(), person, workspaceSource(), sign(t, signer, person, workspaceGrant(), time.Minute))
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnavailable), "Admit() error = %v, want Unavailable", err)
	})
	t.Run("no answer", func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
		t.Cleanup(server.Close)
		t.Cleanup(func() { close(release) })
		admission, err := New(Config{JWKSURL: server.URL + grant.JWKSPath, Issuer: issuer, Audience: audience}, &http.Client{Timeout: 50 * time.Millisecond})
		require.NoError(t, err)
		signer := newSigner(t, issuer, audience)
		_, err = admission.Admit(t.Context(), person, workspaceSource(), sign(t, signer, person, workspaceGrant(), time.Minute))
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnavailable), "Admit() error = %v, want Unavailable", err)
		require.NotErrorIs(t, err, context.DeadlineExceeded, "the caller's deadline did not pass")
	})
	t.Run("fresh keys still admit, stale keys do not", func(t *testing.T) {
		signer := newSigner(t, issuer, audience)
		admission, keys, at := newTestAdmission(t, signer)
		_, err := admission.Admit(t.Context(), person, workspaceSource(), sign(t, signer, person, workspaceGrant(), time.Minute))
		require.NoError(t, err)
		keys.takeDown()
		_, err = admission.Admit(t.Context(), person, workspaceSource(), sign(t, signer, person, workspaceGrant(), time.Minute))
		require.NoError(t, err, "keys fetched within keysMaxAge admit while the issuer is down")
		at.advance(keysMaxAge)
		_, err = admission.Admit(t.Context(), person, workspaceSource(), sign(t, signer, person, workspaceGrant(), time.Minute))
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnavailable), "Admit() error = %v, want Unavailable", err)
	})
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	for name, config := range map[string]Config{
		"no URL":         {Issuer: issuer, Audience: audience},
		"relative URL":   {JWKSURL: "/jwks.json", Issuer: issuer, Audience: audience},
		"another scheme": {JWKSURL: "file:///jwks.json", Issuer: issuer, Audience: audience},
		"no issuer":      {JWKSURL: "https://workspaces.example.test/jwks.json", Audience: audience},
		"no audience":    {JWKSURL: "https://workspaces.example.test/jwks.json", Issuer: issuer},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(config, nil)
			require.Error(t, err)
		})
	}
}

// tamper swaps the grant's payload for another person's, keeping the
// signature.
func tamper(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	other := strings.Split(sign(t, newSigner(t, issuer, audience), "mallory@example.test", workspaceGrant(), time.Minute), ".")
	return parts[0] + "." + other[1] + "." + parts[2]
}
