package e2e_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Keep the parent sequential: the fixture installs a temporary HTTPS root in
// the gateway and restores its configuration before other parallel E2Es run.
func TestEgressPolicyCredentialDelivery(t *testing.T) {
	target := interactionTarget(t)
	kube := interactionKubeClient(t)
	const token = "explicit-policy-test-token"
	destination := startAuthenticatedEgressDestination(t, kube, token)
	image := publishSkillImage(t)
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		modelURL, model, runs := startEgressSkillMock(t, harness.skillsRoot(), destination)
		config := harness.createModel(t, kube, modelURL, nil)
		template := &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{GenerateName: "egress-credentials-", Namespace: "kagent", Labels: harness.labels()}, Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: config.Name}, SystemPrompt: "Load the egress skill and run its probe.",
			Skills: []v1alpha3.AgentTemplateSkill{{Name: "egress", Source: v1alpha3.ArtifactSource{OCI: image, Path: "egress"}}},
		}}
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{GenerateName: "egress-header-", Namespace: "kagent"}, StringData: map[string]string{"token": token}}
		require.NoError(t, kube.Create(t.Context(), secret))
		t.Cleanup(func() { assert.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), secret))) })
		ref := corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token"}
		var refs []corev1.LocalObjectReference
		for _, authentication := range []*v1alpha3.EgressAuthentication{
			{Header: "Authorization", Prefix: new("Bearer "), SecretKeyRef: ref},
			{Header: "X-API-Key", SecretKeyRef: ref},
		} {
			policy := &v1alpha3.EgressPolicy{ObjectMeta: metav1.ObjectMeta{GenerateName: "egress-header-", Namespace: "kagent"}, Spec: v1alpha3.EgressPolicySpec{Destinations: []v1alpha3.EgressDestination{{Origin: destination, Authentication: authentication}}}}
			require.NoError(t, kube.Create(t.Context(), policy))
			t.Cleanup(func() { assert.NoError(t, ctrlclient.IgnoreNotFound(kube.Delete(context.Background(), policy))) })
			refs = append(refs, corev1.LocalObjectReference{Name: policy.Name})
		}
		run := runs("live")
		finish := runEgressSkill(t, fixture, model, run)
		run.expect(t, "initial", "DENIED")
		agent := &v1alpha3.Agent{}
		require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: template.Name}, agent))
		agent.Spec.EgressPolicyRefs = refs
		require.NoError(t, kube.Update(t.Context(), agent))
		run.setPhase("authenticated")
		run.expect(t, "authenticated", "ALLOWED")
		// The HTTPS server returns success only when both placeholders have been
		// replaced from the Secret, with and without a prefix respectively.
		finish()
	})
}

func startAuthenticatedEgressDestination(t *testing.T, kube ctrlclient.Client, token string) string {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-API-Key") != token {
			http.Error(w, "credential injection required", http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, "EGRESS_DESTINATION_AUTHENTICATED\n")
	}))
	require.NoError(t, server.Listener.Close())
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server.Listener = listener
	origin := reachableServerURL(t, "https://"+listener.Addr().String(), "")
	parsed, err := url.Parse(origin)
	require.NoError(t, err)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "kagent E2E CA"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "kagent E2E origin"}, DNSNames: []string{parsed.Hostname()}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore: ca.NotBefore, NotAfter: ca.NotAfter,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, ca, &key.PublicKey, caKey)
	require.NoError(t, err)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der, caDER}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	// Prove a successful fetch cannot be produced by a missing/unchanged header.
	for _, value := range []string{"", "kagent-credential-injected"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+net.JoinHostPort("127.0.0.1", parsed.Port()), nil)
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+value)
		request.Header.Set("X-API-Key", value)
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusForbidden, response.StatusCode)
	}
	trustEgressTestOrigin(t, kube, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})))
	return origin
}

// Configure only the gateway's destination TLS trust. Actor identity, policy
// lookup, credential provider, and guest MITM trust use the normal installation.
func trustEgressTestOrigin(t *testing.T, kube ctrlclient.Client, certificate string) {
	t.Helper()
	deployments := &appsv1.DeploymentList{}
	require.NoError(t, kube.List(t.Context(), deployments, ctrlclient.InNamespace("ate-system"), ctrlclient.MatchingLabels{"app": "atenet-egress"}))
	require.Len(t, deployments.Items, 1)
	deployment := &deployments.Items[0]
	var configName string
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == "config" && volume.ConfigMap != nil {
			configName = volume.ConfigMap.Name
		}
	}
	require.NotEmpty(t, configName)
	config := &corev1.ConfigMap{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: deployment.Namespace, Name: configName}, config))
	original := config.DeepCopy()
	patched, restored, err := egressGatewayTrustConfig(config.Data, certificate)
	require.NoError(t, err)
	config.Data = patched
	rollout := func(ctx context.Context) error {
		current := &appsv1.Deployment{}
		deploymentKey := ctrlclient.ObjectKeyFromObject(deployment)
		if err := kube.Get(ctx, deploymentKey, current); err != nil {
			return err
		}
		before := current.DeepCopy()
		if current.Spec.Template.Annotations == nil {
			current.Spec.Template.Annotations = map[string]string{}
		}
		current.Spec.Template.Annotations["kagent.dev/egress-test-rollout"] = uuid.NewString()
		if err := kube.Patch(ctx, current, ctrlclient.MergeFrom(before)); err != nil {
			return err
		}
		return wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			observed := &appsv1.Deployment{}
			if err := kube.Get(ctx, deploymentKey, observed); err != nil {
				return false, err
			}
			return observed.Status.ObservedGeneration >= current.Generation && observed.Status.UpdatedReplicas == *observed.Spec.Replicas && observed.Status.AvailableReplicas == *observed.Spec.Replicas && observed.Status.Replicas == *observed.Spec.Replicas, nil
		})
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		current := &corev1.ConfigMap{}
		if !assert.NoError(t, kube.Get(ctx, ctrlclient.ObjectKeyFromObject(original), current), "get gateway config for cleanup") {
			return
		}
		before := current.DeepCopy()
		current.Data["config.yaml"] = restored["config.yaml"]
		delete(current.Data, egressTestRootFile)
		if !assert.NoError(t, kube.Patch(ctx, current, ctrlclient.MergeFrom(before)), "restore gateway config") {
			return
		}
		assert.NoError(t, rollout(ctx), "roll out restored gateway config")
	})
	require.NoError(t, kube.Patch(t.Context(), config, ctrlclient.MergeFrom(original)))
	require.NoError(t, rollout(t.Context()))
}

const egressTestRootFile = "egress-test-root.pem"

// Patch only dynamic HTTPS destinations, preserving the gateway's other policies.
// The restore configuration also removes test trust left by an interrupted run.
func egressGatewayTrustConfig(data map[string]string, certificate string) (patched, restored map[string]string, err error) {
	var config map[string]any
	if err := yaml.Unmarshal([]byte(data["config.yaml"]), &config); err != nil {
		return nil, nil, fmt.Errorf("decode gateway config: %w", err)
	}
	var backends []map[string]any
	binds, _ := config["binds"].([]any)
	for _, value := range binds {
		bind, _ := value.(map[string]any)
		if bind["mode"] != "internal" {
			continue
		}
		listeners, _ := bind["listeners"].([]any)
		for _, value := range listeners {
			listener, _ := value.(map[string]any)
			if listener["protocol"] != "HTTPS" {
				continue
			}
			routes, _ := listener["routes"].([]any)
			for _, value := range routes {
				route, _ := value.(map[string]any)
				items, _ := route["backends"].([]any)
				for _, value := range items {
					backend, _ := value.(map[string]any)
					if _, dynamic := backend["dynamic"]; dynamic {
						backends = append(backends, backend)
					}
				}
			}
		}
	}
	if len(backends) != 1 {
		return nil, nil, fmt.Errorf("expected one dynamic HTTPS backend, found %d", len(backends))
	}
	backend := backends[0]
	path := []string{"policies", "backendTLS", "root"}
	root, _, err := unstructured.NestedString(backend, path...)
	if err != nil {
		return nil, nil, fmt.Errorf("read gateway TLS root: %w", err)
	}
	restored = maps.Clone(data)
	delete(restored, egressTestRootFile)
	const testRoot = "/etc/agentgateway/" + egressTestRootFile
	if root == testRoot {
		unstructured.RemoveNestedField(backend, path...)
		cleaned, err := yaml.Marshal(config)
		if err != nil {
			return nil, nil, fmt.Errorf("encode restored gateway config: %w", err)
		}
		restored["config.yaml"] = string(cleaned)
	}
	if err := unstructured.SetNestedField(backend, testRoot, path...); err != nil {
		return nil, nil, fmt.Errorf("set gateway TLS root: %w", err)
	}
	updated, err := yaml.Marshal(config)
	if err != nil {
		return nil, nil, fmt.Errorf("encode gateway config: %w", err)
	}
	patched = maps.Clone(restored)
	patched["config.yaml"] = string(updated)
	patched[egressTestRootFile] = certificate
	return patched, restored, nil
}

func TestEgressGatewayTrustConfig(t *testing.T) {
	const gateway = `frontendPolicies:
  substrateEgressActorResolution:
    policies:
      backendTLS: {root: /identity-ca.pem}
binds:
- mode: internal
  listeners:
  - protocol: HTTP
    routes:
    - backends:
      - dynamic: {}
  - protocol: HTTPS
    tls: {mode: dynamicCa, cert: /mitm.pem}
    routes:
    - policies:
        substrateEgress:
          policies:
            backendTLS: {root: /control-plane-ca.pem}
      backends:
      - dynamic: {}
        policies:
          backendTLS: %s
`
	for _, tc := range []struct {
		name, tls, restoredTLS string
	}{
		{"clean", "{}", "{}"},
		{"interrupted", "{root: /etc/agentgateway/egress-test-root.pem}", "{}"},
		{"existing trust", "{root: /custom-ca.pem, hostname: example.com}", "{root: /custom-ca.pem, hostname: example.com}"},
		{"block format", "\n            root: /etc/agentgateway/egress-test-root.pem", "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := map[string]string{"config.yaml": fmt.Sprintf(gateway, tc.tls), egressTestRootFile: "leftover CA", "other.pem": "keep"}
			before := maps.Clone(input)
			patched, restored, err := egressGatewayTrustConfig(input, "new CA")
			require.NoError(t, err)
			require.Equal(t, before, input, "patch must not mutate the saved config")
			require.Equal(t, "new CA", patched[egressTestRootFile])
			require.Equal(t, "keep", patched["other.pem"])
			require.NotContains(t, restored, egressTestRootFile)
			var want, got map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(fmt.Sprintf(gateway, tc.restoredTLS)), &want))
			require.NoError(t, yaml.Unmarshal([]byte(restored["config.yaml"]), &got))
			require.Equal(t, want, got, "cleanup must preserve unrelated TLS policy")
			repeated, _, err := egressGatewayTrustConfig(patched, "replacement CA")
			require.NoError(t, err)
			require.Equal(t, patched["config.yaml"], repeated["config.yaml"], "reapplying trust must be idempotent")
			require.Equal(t, "replacement CA", repeated[egressTestRootFile])
			require.Contains(t, patched["config.yaml"], "root: /etc/agentgateway/egress-test-root.pem")
			require.Contains(t, patched["config.yaml"], "root: /identity-ca.pem")
			require.Contains(t, patched["config.yaml"], "root: /control-plane-ca.pem")
		})
	}
	for _, input := range []string{"[invalid", "binds: []"} {
		_, _, err := egressGatewayTrustConfig(map[string]string{"config.yaml": input}, "CA")
		require.Error(t, err)
	}
}
