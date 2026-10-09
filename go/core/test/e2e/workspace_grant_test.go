package e2e_test

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/test/grant"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The lane configures the controller's jwt Session admission with these
// (.github/workflows/ci.yaml): the JWKS at a fixed Service the suite points at
// its own grant signer, and the issuer and audience the signer's grants name.
const (
	workspaceGrantService  = "e2e-grant-jwks"
	workspaceGrantIssuer   = "https://grant-issuer.e2e.kagent.dev"
	workspaceGrantAudience = "kagent-e2e"
	// workspaceGrantSubject is the caller every e2e request names (x-user-id).
	workspaceGrantSubject = "e2e"
	// workspaceGrantTTL outlasts a create's retries.
	workspaceGrantTTL = 10 * time.Minute
)

var (
	workspaceSignerOnce sync.Once
	workspaceSigner     *grant.Signer
	workspaceSignerErr  error
)

// workspaceGrantSigner is the suite's one grant signer, its JWKS served from
// the test host behind the Service the controller's admission fetches it from.
// The server lives as long as the test binary, since every workspace test
// signs with the same key.
func workspaceGrantSigner(t *testing.T) *grant.Signer {
	t.Helper()
	workspaceSignerOnce.Do(func() {
		workspaceSigner, workspaceSignerErr = startWorkspaceGrantSigner(interactionKubeClient(t))
	})
	require.NoError(t, workspaceSignerErr, "serve the grant signer's JWKS")
	return workspaceSigner
}

func startWorkspaceGrantSigner(kube ctrlclient.Client) (*grant.Signer, error) {
	signer, err := grant.NewSigner(workspaceGrantIssuer, workspaceGrantAudience)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	go func() { _ = http.Serve(listener, signer.Handler()) }()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return nil, err
	}
	number, err := strconv.ParseInt(port, 10, 32)
	if err != nil {
		return nil, err
	}
	host := kagentenv.KagentLocalHost.Get()
	if host == "" {
		host = "172.17.0.1"
	}
	address := net.ParseIP(host)
	if address == nil {
		return nil, &net.AddrError{Err: "the grant signer's JWKS needs the test host's IP address", Addr: host}
	}
	addressType := discoveryv1.AddressTypeIPv4
	if address.To4() == nil {
		addressType = discoveryv1.AddressTypeIPv6
	}
	ctx := context.Background()
	// A run before this one left the Service pointing at its own port.
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: workspaceGrantService, Namespace: "kagent"}}
	if err := kube.Delete(ctx, service); err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	service.Spec = corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt32(int32(number))}}}
	if err := createWhenGone(ctx, kube, service); err != nil {
		return nil, err
	}
	endpoints := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: service.Name, Namespace: service.Namespace,
			Labels:          map[string]string{discoveryv1.LabelServiceName: service.Name, discoveryv1.LabelManagedBy: "kagent-e2e"},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Service", Name: service.Name, UID: service.UID}}},
		AddressType: addressType,
		Ports:       []discoveryv1.EndpointPort{{Name: new("http"), Port: new(int32(number)), Protocol: new(corev1.ProtocolTCP)}},
		Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{address.String()}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}}},
	}
	if err := createWhenGone(ctx, kube, endpoints); err != nil {
		return nil, err
	}
	return signer, nil
}

// createWhenGone creates object, retrying while a deleted one of its name is
// still being removed.
func createWhenGone(ctx context.Context, kube ctrlclient.Client, object ctrlclient.Object) error {
	return wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		err := kube.Create(ctx, object)
		if apierrors.IsAlreadyExists(err) {
			return false, nil
		}
		return err == nil, err
	})
}

// workspaceGrant signs a grant admitting the e2e caller to the fixture's
// volume with the workspace layout's mounts.
func (f *workspaceFixture) workspaceGrant(t *testing.T) string {
	t.Helper()
	token, err := workspaceGrantSigner(t).SignGrant(workspaceGrantSubject, grant.Grant{
		Volume:  grant.Volume{Driver: f.Volume.Driver, Handle: f.Volume.Handle},
		Mounts:  []grant.Mount{{SubPath: "sessions/${SESSION_ID}"}, {SubPath: "mirrors", ReadOnly: true}},
		Changed: f.Changed,
	}, workspaceGrantTTL)
	require.NoError(t, err)
	return token
}
