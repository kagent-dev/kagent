package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// findDeployment extracts the agent Deployment from a translator output bundle.
// Translator output is a heterogeneous slice of K8s objects; the agent pod
// spec lives on the Deployment.
func findDeployment(t *testing.T, outputs *translator.AgentOutputs) *appsv1.Deployment {
	t.Helper()
	for _, obj := range outputs.Manifest {
		if d, ok := obj.(*appsv1.Deployment); ok {
			return d
		}
	}
	t.Fatal("Deployment not found in manifest")
	return nil
}

// hasVolumeForSecret returns the volume in the deployment that mounts the
// named Secret, or nil if no such volume is present. Used to assert TLS CA
// Secrets land on the agent pod when RMS.Spec.TLS or ModelConfig.Spec.TLS
// reference them.
func hasVolumeForSecret(dep *appsv1.Deployment, secretName string) *corev1.Volume {
	for i, v := range dep.Spec.Template.Spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName == secretName {
			return &dep.Spec.Template.Spec.Volumes[i]
		}
	}
	return nil
}

// Test_AdkApiTranslator_RMSTLS_DisableVerify exercises the simplest TLS path
// on a RemoteMCPServer: spec.tls.disableVerify=true with no CA Secret. The
// agent's StreamableHTTP connection params should carry the disable-verify
// flag, and no CA volume should be mounted (nothing to mount).
func Test_AdkApiTranslator_RMSTLS_DisableVerify(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tls-test"}}
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "tls-test"},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4o",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}
	rms := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "self-signed-upstream", Namespace: "tls-test"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "Self-signed test fixture",
			URL:         "https://upstream.example.com/mcp",
			TLS: &v1alpha2.TLSConfig{
				DisableVerify: true,
			},
		},
	}
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "tls-test"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Description: "Agent",
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "You are an agent",
				ModelConfig:   "model",
				Tools: []*v1alpha2.Tool{{
					Type: v1alpha2.ToolProviderType_McpServer,
					McpServer: &v1alpha2.McpServerTool{
						TypedReference: v1alpha2.TypedReference{
							Kind:     "RemoteMCPServer",
							ApiGroup: "kagent.dev",
							Name:     "self-signed-upstream",
						},
					},
				}},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns, modelConfig, rms, agent).
		Build()

	trans := translator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "tls-test", Name: "model"},
		nil, "", nil,
	)
	outputs, err := translator.TranslateAgent(context.Background(), trans, agent)
	require.NoError(t, err)
	require.NotNil(t, outputs.Config)

	require.Len(t, outputs.Config.HttpTools, 1, "Expected one Streamable HTTP tool")
	params := outputs.Config.HttpTools[0].Params
	require.NotNil(t, params.TLSInsecureSkipVerify, "TLSInsecureSkipVerify should be set when spec.tls is present")
	assert.True(t, *params.TLSInsecureSkipVerify, "disableVerify should propagate")
	assert.Nil(t, params.TLSCACertPath, "No CA path expected without CACertSecretRef")

	// No CA Secret → no extra TLS volume on the deployment.
	dep := findDeployment(t, outputs)
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Secret != nil {
			assert.NotContains(t, v.Name, "tls-ca-", "No TLS volume expected when no CACertSecretRef is set")
		}
	}
}

// Test_AdkApiTranslator_RMSTLS_CustomCA exercises the production path: an
// RemoteMCPServer pinning a CA Secret produces (a) TLSCACertPath on the wire config so
// the runtime initializes its trust store from disk, and (b) a per-Secret
// read-only volume + mount on the agent pod so the file is actually present.
func Test_AdkApiTranslator_RMSTLS_CustomCA(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tls-test"}}
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "rms-corp-ca", Namespace: "tls-test"},
		Data:       map[string][]byte{"ca.crt": []byte("FAKE CA PEM")},
	}
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "tls-test"},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4o",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}
	rms := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "corp-upstream", Namespace: "tls-test"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "Corporate-CA upstream",
			URL:         "https://mcp.corp.internal/mcp",
			TLS: &v1alpha2.TLSConfig{
				CACertSecretRef: "rms-corp-ca",
				CACertSecretKey: "ca.crt",
			},
		},
	}
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "tls-test"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Description: "Agent",
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "You are an agent",
				ModelConfig:   "model",
				Tools: []*v1alpha2.Tool{{
					Type: v1alpha2.ToolProviderType_McpServer,
					McpServer: &v1alpha2.McpServerTool{
						TypedReference: v1alpha2.TypedReference{
							Kind:     "RemoteMCPServer",
							ApiGroup: "kagent.dev",
							Name:     "corp-upstream",
						},
					},
				}},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns, caSecret, modelConfig, rms, agent).
		Build()

	trans := translator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "tls-test", Name: "model"},
		nil, "", nil,
	)
	outputs, err := translator.TranslateAgent(context.Background(), trans, agent)
	require.NoError(t, err)

	require.Len(t, outputs.Config.HttpTools, 1)
	params := outputs.Config.HttpTools[0].Params
	require.NotNil(t, params.TLSCACertPath, "TLSCACertPath should be set when CA Secret is referenced")
	assert.Contains(t, *params.TLSCACertPath, "rms-corp-ca/ca.crt",
		"Cert path should embed the Secret name (operator-readable, per-Secret bucket)")

	dep := findDeployment(t, outputs)
	v := hasVolumeForSecret(dep, "rms-corp-ca")
	require.NotNil(t, v, "Deployment should mount the CA Secret as a volume")
	require.NotNil(t, v.Secret.DefaultMode)
	assert.Equal(t, int32(0444), *v.Secret.DefaultMode, "CA Secret volume should be read-only 0444")

	// Mount path on the agent container should match the cert path's
	// directory and be marked read-only.
	var mountFound bool
	for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.Name == v.Name {
			assert.True(t, m.ReadOnly, "CA mount should be read-only")
			assert.Contains(t, m.MountPath, "rms-corp-ca")
			mountFound = true
		}
	}
	assert.True(t, mountFound, "Agent container should mount the CA volume")
}

// Test_AdkApiTranslator_RMSTLS_SharedSecretAcrossRMSs guards a regression
// where two RemoteMCPServers in the same agent reference the same CA
// Secret. translateRemoteMCPServerTarget calls addTLSConfiguration once
// per RemoteMCPServer directly on the already-merged modelDeploymentData, so without
// idempotency we'd produce two Volume entries with the same Name and the
// API server would reject the Deployment with `Pod.spec.volumes[N].name:
// Duplicate value`. The "all our internal MCP services share one
// corporate-CA bundle" topology is the realistic shape that triggers it.
func Test_AdkApiTranslator_RMSTLS_SharedSecretAcrossRMSs(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tls-test"}}
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "corp-ca", Namespace: "tls-test"},
		Data:       map[string][]byte{"ca.crt": []byte("FAKE CA PEM")},
	}
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "tls-test"},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4o",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}
	sharedTLS := &v1alpha2.TLSConfig{
		CACertSecretRef: "corp-ca",
		CACertSecretKey: "ca.crt",
	}
	rmsA := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "rms-a", Namespace: "tls-test"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "RMS A behind corp CA",
			URL:         "https://a.corp.internal/mcp",
			TLS:         sharedTLS,
		},
	}
	rmsB := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "rms-b", Namespace: "tls-test"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "RMS B behind corp CA",
			URL:         "https://b.corp.internal/mcp",
			TLS:         sharedTLS,
		},
	}
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "tls-test"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Description: "Agent",
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "You are an agent",
				ModelConfig:   "model",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedReference: v1alpha2.TypedReference{
								Kind: "RemoteMCPServer", ApiGroup: "kagent.dev", Name: "rms-a",
							},
						},
					},
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedReference: v1alpha2.TypedReference{
								Kind: "RemoteMCPServer", ApiGroup: "kagent.dev", Name: "rms-b",
							},
						},
					},
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns, caSecret, modelConfig, rmsA, rmsB, agent).
		Build()

	trans := translator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "tls-test", Name: "model"},
		nil, "", nil,
	)
	outputs, err := translator.TranslateAgent(context.Background(), trans, agent)
	require.NoError(t, err)

	dep := findDeployment(t, outputs)

	// Count Volumes that mount the shared Secret. Must be exactly one;
	// duplicates would fail kubelet admission.
	matchingVolumes := 0
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName == "corp-ca" {
			matchingVolumes++
		}
	}
	assert.Equal(t, 1, matchingVolumes, "Same Secret referenced from multiple RMSs must produce exactly one Volume")

	// Both RMSs' connection params should still point at the same cert
	// file — the mount is shared but every transport reads from the
	// same on-disk path.
	require.Len(t, outputs.Config.HttpTools, 2)
	pathA := outputs.Config.HttpTools[0].Params.TLSCACertPath
	pathB := outputs.Config.HttpTools[1].Params.TLSCACertPath
	require.NotNil(t, pathA)
	require.NotNil(t, pathB)
	assert.Equal(t, *pathA, *pathB, "Both RMSs sharing a Secret should resolve to the same cert path")
}

// Test_AdkApiTranslator_RMSTLS_EmptyTLSStructIsNoOp guards against the
// trap where a non-nil but all-zero `Spec.TLS: {}` produces wire fields
// (`tls_insecure_skip_verify: false`, `tls_disable_system_cas: false`)
// that would flip the Python runtime out of its no-op short-circuit and
// silently swap google-adk's default httpx client factory for kagent's.
// Both factories produce the same SSL behavior here but differ on
// timeout / redirect defaults; the principle of least surprise says
// `{}` should be indistinguishable from `nil`.
// Test_AdkApiTranslator_RMSTLS_AbsentTLSIsNoOp asserts the symmetric case
// to RMSTLS_EmptyTLSStructIsNoOp: an HTTPS RMS with spec.tls entirely
// unset is admitted and behaves identically to spec.tls: {}. The CRD only
// rejects spec.tls on http:// URLs; HTTPS with no opinion defaults to
// system trust on the agent side.
func Test_AdkApiTranslator_RMSTLS_AbsentTLSIsNoOp(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tls-test"}}
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "tls-test"},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4o",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}
	rms := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "upstream", Namespace: "tls-test"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "Upstream with no TLS opinion",
			URL:         "https://upstream.example.com/mcp",
		},
	}
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "tls-test"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Description: "Agent",
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "You are an agent",
				ModelConfig:   "model",
				Tools: []*v1alpha2.Tool{{
					Type: v1alpha2.ToolProviderType_McpServer,
					McpServer: &v1alpha2.McpServerTool{
						TypedReference: v1alpha2.TypedReference{
							Kind: "RemoteMCPServer", ApiGroup: "kagent.dev", Name: "upstream",
						},
					},
				}},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns, modelConfig, rms, agent).
		Build()

	trans := translator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "tls-test", Name: "model"},
		nil, "", nil,
	)
	outputs, err := translator.TranslateAgent(context.Background(), trans, agent)
	require.NoError(t, err)

	require.Len(t, outputs.Config.HttpTools, 1)
	params := outputs.Config.HttpTools[0].Params
	assert.Nil(t, params.TLSInsecureSkipVerify,
		"Absent TLS must not emit explicit booleans on the wire")
	assert.Nil(t, params.TLSDisableSystemCAs,
		"Absent TLS must not emit explicit booleans on the wire")
	assert.Nil(t, params.TLSCACertPath)
}

func Test_AdkApiTranslator_RMSTLS_EmptyTLSStructIsNoOp(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tls-test"}}
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "tls-test"},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4o",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	}
	rms := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "upstream", Namespace: "tls-test"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "Upstream with empty TLS struct",
			URL:         "https://upstream.example.com/mcp",
			TLS:         &v1alpha2.TLSConfig{},
		},
	}
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "tls-test"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Description: "Agent",
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "You are an agent",
				ModelConfig:   "model",
				Tools: []*v1alpha2.Tool{{
					Type: v1alpha2.ToolProviderType_McpServer,
					McpServer: &v1alpha2.McpServerTool{
						TypedReference: v1alpha2.TypedReference{
							Kind: "RemoteMCPServer", ApiGroup: "kagent.dev", Name: "upstream",
						},
					},
				}},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns, modelConfig, rms, agent).
		Build()

	trans := translator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "tls-test", Name: "model"},
		nil, "", nil,
	)
	outputs, err := translator.TranslateAgent(context.Background(), trans, agent)
	require.NoError(t, err)

	require.Len(t, outputs.Config.HttpTools, 1)
	params := outputs.Config.HttpTools[0].Params
	assert.Nil(t, params.TLSInsecureSkipVerify,
		"Empty TLS struct must not emit explicit booleans on the wire")
	assert.Nil(t, params.TLSDisableSystemCAs,
		"Empty TLS struct must not emit explicit booleans on the wire")
	assert.Nil(t, params.TLSCACertPath)
}

// Test_AdkApiTranslator_RMSTLS_CoexistsWithModelConfigTLS verifies the
// reason the volume-naming rework was necessary: an agent that combines a
// ModelConfig TLS CA with one or more RemoteMCPServer TLS CAs must end up
// with every CA mounted on the pod, not just one. Before per-Secret naming
// the merge dedupe would silently drop later contributors because they all
// declared the same volume name.
func Test_AdkApiTranslator_RMSTLS_CoexistsWithModelConfigTLS(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tls-test"}}
	modelCASecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "model-ca", Namespace: "tls-test"},
		Data:       map[string][]byte{"ca.crt": []byte("FAKE MODEL CA PEM")},
	}
	rmsACASecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "rms-a-ca", Namespace: "tls-test"},
		Data:       map[string][]byte{"ca.crt": []byte("FAKE RMS A CA PEM")},
	}
	rmsBCASecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "rms-b-ca", Namespace: "tls-test"},
		Data:       map[string][]byte{"ca.crt": []byte("FAKE RMS B CA PEM")},
	}
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "tls-test"},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4o",
			Provider: v1alpha2.ModelProviderOpenAI,
			TLS: &v1alpha2.TLSConfig{
				CACertSecretRef: "model-ca",
				CACertSecretKey: "ca.crt",
			},
		},
	}
	rmsA := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "rms-a", Namespace: "tls-test"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "RMS A",
			URL:         "https://a.example.com/mcp",
			TLS: &v1alpha2.TLSConfig{
				CACertSecretRef: "rms-a-ca",
				CACertSecretKey: "ca.crt",
			},
		},
	}
	rmsB := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "rms-b", Namespace: "tls-test"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "RMS B",
			URL:         "https://b.example.com/mcp",
			TLS: &v1alpha2.TLSConfig{
				CACertSecretRef: "rms-b-ca",
				CACertSecretKey: "ca.crt",
			},
		},
	}
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "tls-test"},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Description: "Agent",
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "You are an agent",
				ModelConfig:   "model",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedReference: v1alpha2.TypedReference{
								Kind: "RemoteMCPServer", ApiGroup: "kagent.dev", Name: "rms-a",
							},
						},
					},
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedReference: v1alpha2.TypedReference{
								Kind: "RemoteMCPServer", ApiGroup: "kagent.dev", Name: "rms-b",
							},
						},
					},
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns, modelCASecret, rmsACASecret, rmsBCASecret, modelConfig, rmsA, rmsB, agent).
		Build()

	trans := translator.NewAdkApiTranslator(
		kubeClient,
		types.NamespacedName{Namespace: "tls-test", Name: "model"},
		nil, "", nil,
	)
	outputs, err := translator.TranslateAgent(context.Background(), trans, agent)
	require.NoError(t, err)

	dep := findDeployment(t, outputs)

	// All three Secrets must be mounted on the agent pod.
	for _, secretName := range []string{"model-ca", "rms-a-ca", "rms-b-ca"} {
		v := hasVolumeForSecret(dep, secretName)
		assert.NotNilf(t, v, "Secret %q should be mounted on the agent pod", secretName)
	}

	// The three volumes must have distinct names and distinct mount paths
	// so the merge dedupe doesn't silently drop any. Volume name + mount
	// path are derived deterministically from the Secret name.
	tlsVolumeNames := map[string]struct{}{}
	tlsMountPaths := map[string]struct{}{}
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName != "" {
			switch v.Secret.SecretName {
			case "model-ca", "rms-a-ca", "rms-b-ca":
				tlsVolumeNames[v.Name] = struct{}{}
			}
		}
	}
	for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
		for n := range tlsVolumeNames {
			if m.Name == n {
				tlsMountPaths[m.MountPath] = struct{}{}
			}
		}
	}
	assert.Len(t, tlsVolumeNames, 3, "Three TLS Secrets must produce three distinct volume names")
	assert.Len(t, tlsMountPaths, 3, "Three TLS Secrets must produce three distinct mount paths")
}

// Test_AdkApiTranslator_RMSTLS_SecretHashChangesAgentConfigHash pins the
// cert-rotation behavior: an in-place rotation of the RMS TLS Secret
// (same Secret name, new PEM, status SecretHash flipped by the
// controller) must change the agent's kagent.dev/config-hash so the
// deployment rolls. Without this, agent pods retain the old cert loaded
// at process startup via ssl.create_default_context.
func Test_AdkApiTranslator_RMSTLS_SecretHashChangesAgentConfigHash(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	build := func(rmsHash string) string {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "rotate-test"}}
		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "corp-ca", Namespace: "rotate-test"},
			Data:       map[string][]byte{"ca.crt": []byte("FAKE CA PEM")},
		}
		modelConfig := &v1alpha2.ModelConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "rotate-test"},
			Spec:       v1alpha2.ModelConfigSpec{Model: "gpt-4o", Provider: v1alpha2.ModelProviderOpenAI},
		}
		rms := &v1alpha2.RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "corp-upstream", Namespace: "rotate-test"},
			Spec: v1alpha2.RemoteMCPServerSpec{
				Description: "Corp upstream",
				URL:         "https://mcp.corp.internal/mcp",
				TLS: &v1alpha2.TLSConfig{
					CACertSecretRef: "corp-ca",
					CACertSecretKey: "ca.crt",
				},
			},
			Status: v1alpha2.RemoteMCPServerStatus{SecretHash: rmsHash},
		}
		agent := &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "rotate-test"},
			Spec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Description: "Agent",
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					SystemMessage: "System",
					ModelConfig:   "model",
					Tools: []*v1alpha2.Tool{{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedReference: v1alpha2.TypedReference{
								Kind: "RemoteMCPServer", ApiGroup: "kagent.dev", Name: "corp-upstream",
							},
						},
					}},
				},
			},
		}

		kube := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(ns, caSecret, modelConfig, rms, agent).
			Build()
		trans := translator.NewAdkApiTranslator(
			kube,
			types.NamespacedName{Namespace: "rotate-test", Name: "model"},
			nil, "", nil,
		)
		outputs, err := translator.TranslateAgent(context.Background(), trans, agent)
		require.NoError(t, err)
		dep := findDeployment(t, outputs)
		return dep.Spec.Template.Annotations["kagent.dev/config-hash"]
	}

	preRotate := build("deadbeef")
	postRotate := build("cafef00d")
	assert.NotEqual(t, preRotate, postRotate,
		"agent config-hash must change when RMS Status.SecretHash rotates")
}
