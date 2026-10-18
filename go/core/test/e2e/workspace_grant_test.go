package e2e_test

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
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
	if err := exposeOnService(context.Background(), kube, workspaceGrantService, 80, listener); err != nil {
		return nil, err
	}
	return signer, nil
}

// exposeOnService points the Service name of namespace kagent, on port, at
// listener on the test host, so the cluster reaches it under a fixed name.
func exposeOnService(ctx context.Context, kube ctrlclient.Client, name string, port int32, listener net.Listener) error {
	_, hostPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return err
	}
	number, err := strconv.ParseInt(hostPort, 10, 32)
	if err != nil {
		return err
	}
	host := kagentenv.KagentLocalHost.Get()
	if host == "" {
		host = "172.17.0.1"
	}
	address := net.ParseIP(host)
	if address == nil {
		return &net.AddrError{Err: "the Service " + name + " needs the test host's IP address", Addr: host}
	}
	addressType := discoveryv1.AddressTypeIPv4
	if address.To4() == nil {
		addressType = discoveryv1.AddressTypeIPv6
	}
	// A run before this one left the Service pointing at its own port.
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kagent"}}
	if err := kube.Delete(ctx, service); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	service.Spec = corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "port", Port: port, TargetPort: intstr.FromInt32(int32(number))}}}
	if err := createWhenGone(ctx, kube, service); err != nil {
		return err
	}
	endpoints := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: service.Name, Namespace: service.Namespace,
			Labels:          map[string]string{discoveryv1.LabelServiceName: service.Name, discoveryv1.LabelManagedBy: "kagent-e2e"},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Service", Name: service.Name, UID: service.UID}}},
		AddressType: addressType,
		Ports:       []discoveryv1.EndpointPort{{Name: new("port"), Port: new(int32(number)), Protocol: new(corev1.ProtocolTCP)}},
		Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{address.String()}, Conditions: discoveryv1.EndpointConditions{Ready: new(true)}}},
	}
	return createWhenGone(ctx, kube, endpoints)
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
// volume with the workspace layout's mounts, on the Agents of namespace kagent.
func (f *workspaceFixture) workspaceGrant(t *testing.T) string {
	t.Helper()
	return f.grantFor(t, workspaceGrantSubject)
}

// grantFor signs the workspace grant for the caller subject, naming providers.
func (f *workspaceFixture) grantFor(t *testing.T, subject string, providers ...grant.Provider) string {
	t.Helper()
	token, err := workspaceGrantSigner(t).SignGrant(subject, grant.Grant{
		AgentNamespace: "kagent",
		Volume:         grant.Volume{Driver: f.Volume.Driver, Handle: f.Volume.Handle},
		Mounts:         []grant.Mount{{SubPath: "sessions/${SESSION_ID}"}, {SubPath: "mirrors", ReadOnly: true}},
		Changed:        f.Changed,
		Providers:      providers,
	}, workspaceGrantTTL)
	require.NoError(t, err)
	return token
}

// admit sets a create request's volume source to the workspace layout on the
// fixture's volume, the Session's directory at /workspace and the mirrors
// read-only at /mirrors, admitted with token.
func (f *workspaceFixture) admit(token string) func(*apiv1alpha1.CreateSessionRequest) {
	return func(request *apiv1alpha1.CreateSessionRequest) {
		request.VolumeSource = &apiv1alpha1.SessionVolumeSource{
			Volume: &apiv1alpha1.SessionVolume{CsiDriver: f.Volume.Driver, VolumeHandle: f.Volume.Handle},
			Mounts: []*apiv1alpha1.SessionVolumeMount{
				{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace"},
				{SubPath: "mirrors", MountPath: "/mirrors", ReadOnly: true},
			},
		}
		request.AdmissionToken = token
	}
}
