package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The lane registers the e2e credential provider with Substrate's egress
// gateway (credentialProvider.additionalProviders in .github/workflows/ci.yaml)
// as the provider of credentialProviderAuthority at this Service.
const (
	credentialProviderService   = "e2e-credential-provider"
	credentialProviderPort      = 50051
	credentialProviderAuthority = "credentials.e2e.kagent.dev"
	// The egress gateway's Deployment and the namespace Substrate runs in;
	// the provider answers only the gateway's pod identity.
	egressGatewayNamespace  = "ate-system"
	egressGatewayDeployment = "atenet-egress"
)

// deployCredentialProvider runs the e2e credential provider
// (go/core/test/credentialprovider) behind its Service, refusing the provider
// instances refused, and waits until it serves. It serves with the pod's
// servicedns.podcert.ate.dev certificate, which the signer issues once the
// Service selects the pod, and answers only the egress gateway's
// podidentity.podcert.ate.dev identity.
func deployCredentialProvider(t *testing.T, kube ctrlclient.Client, refused ...string) {
	t.Helper()
	image := kagentenv.E2ECredentialProviderImage.Get()
	if image == "" {
		t.Fatalf("%s must name the e2e credential provider image", kagentenv.E2ECredentialProviderImage.Name())
	}
	require.NoError(t, appsv1.AddToScheme(kube.Scheme()))
	gateway := &appsv1.Deployment{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: egressGatewayNamespace, Name: egressGatewayDeployment}, gateway))
	injector := fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/%s", egressGatewayNamespace, gateway.Spec.Template.Spec.ServiceAccountName)

	labels := map[string]string{"app": credentialProviderService}
	createWorkspaceObject(t, kube, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: credentialProviderService, Namespace: "kagent"},
		Spec: corev1.ServiceSpec{Selector: labels, Ports: []corev1.ServicePort{{
			Name: "grpc", Port: credentialProviderPort, TargetPort: intstr.FromInt32(credentialProviderPort),
		}}},
	})
	podCertificate := func(signer string) corev1.Volume {
		return corev1.Volume{Name: strings.SplitN(signer, ".", 2)[0], VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{
			{PodCertificate: &corev1.PodCertificateProjection{SignerName: signer + "/identity", KeyType: "ECDSAP256", CredentialBundlePath: "credential-bundle.pem"}},
			{ClusterTrustBundle: &corev1.ClusterTrustBundleProjection{
				SignerName:    new(signer + "/identity"),
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"podcert.ate.dev/canarying": "live"}},
				Path:          "trust-bundle.pem",
			}},
		}}}}
	}
	serving, identity := podCertificate("servicedns.podcert.ate.dev"), podCertificate("podidentity.podcert.ate.dev")
	args := []string{"--injector-identity=" + injector, "--authority=" + credentialProviderAuthority}
	if len(refused) != 0 {
		args = append(args, "--refused="+strings.Join(refused, ","))
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: credentialProviderService, Namespace: "kagent"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "provider", Image: image, Args: args,
						Ports: []corev1.ContainerPort{{Name: "grpc", ContainerPort: credentialProviderPort}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: serving.Name, MountPath: "/run/servicedns.podcert.ate.dev", ReadOnly: true},
							{Name: identity.Name, MountPath: "/run/podidentity.podcert.ate.dev", ReadOnly: true},
						},
						ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(credentialProviderPort)}}, PeriodSeconds: 1},
					}},
					Volumes: []corev1.Volume{serving, identity},
				},
			},
		},
	}
	createWorkspaceObject(t, kube, deployment)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		current := &appsv1.Deployment{}
		if assert.NoError(c, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(deployment), current)) {
			assert.Equal(c, int32(1), current.Status.ReadyReplicas, "the credential provider is ready; its pods: %s", credentialProviderPods(t, kube))
		}
	}, 3*time.Minute, time.Second)
}

// credentialProviderPods describes the provider's pods for a failed wait:
// their states and their events, which carry a pod certificate's issuance.
func credentialProviderPods(t *testing.T, kube ctrlclient.Client) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
	defer cancel()
	var pods corev1.PodList
	if err := kube.List(ctx, &pods, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app": credentialProviderService}); err != nil {
		return "unreadable: " + err.Error()
	}
	var events corev1.EventList
	_ = kube.List(ctx, &events, ctrlclient.InNamespace("kagent"))
	report := podStates(pods.Items)
	for _, pod := range pods.Items {
		report += "; " + pod.Name + " events: " + objectEvents(events.Items, "Pod", pod.Name)
	}
	return report
}
