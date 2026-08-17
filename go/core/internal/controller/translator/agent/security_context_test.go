package agent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func assertRestrictedSecurityContext(t *testing.T, securityContext *corev1.SecurityContext) {
	t.Helper()
	require.NotNil(t, securityContext)
	require.NotNil(t, securityContext.AllowPrivilegeEscalation)
	assert.False(t, *securityContext.AllowPrivilegeEscalation)
	require.NotNil(t, securityContext.RunAsNonRoot)
	assert.True(t, *securityContext.RunAsNonRoot)
	require.NotNil(t, securityContext.Capabilities)
	assert.Equal(t, []corev1.Capability{"ALL"}, securityContext.Capabilities.Drop)
	require.NotNil(t, securityContext.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, securityContext.SeccompProfile.Type)
}

func skillsPodRuntime(t *testing.T, agent *v1alpha3.SandboxAgent) *podRuntimeInputs {
	t.Helper()
	manifestCtx := newManifestContext(agent, &resolvedDeployment{Image: "example.com/kagent:test"})
	runtimeInputs, err := buildPodRuntime(manifestCtx, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, runtimeInputs)
	return runtimeInputs
}

func TestSecurityContext_DefaultIsRestricted(t *testing.T) {
	agent := &v1alpha3.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "decl", Namespace: "default"},
		Spec: v1alpha3.AgentSpec{
			Type:        v1alpha3.AgentType_Declarative,
			Declarative: &v1alpha3.DeclarativeAgentSpec{},
		},
	}

	runtimeInputs := skillsPodRuntime(t, agent)

	assert.Nil(t, runtimeInputs.securityContext.Privileged)
	assertRestrictedSecurityContext(t, runtimeInputs.securityContext)
}

// TestSecurityContext_SkillsDefaultNotPrivileged pins that loading skills does
// not by itself request the privileged in-pod srt sandbox: the skills-init
// container clones and pulls skills without elevated privileges, and a
// privileged agent container is rejected by restricted Pod Security admission.
func TestSecurityContext_SkillsDefaultNotPrivileged(t *testing.T) {
	agent := &v1alpha3.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "skills", Namespace: "default"},
		Spec: v1alpha3.AgentSpec{
			Type:        v1alpha3.AgentType_Declarative,
			Declarative: &v1alpha3.DeclarativeAgentSpec{},
			Skills:      &v1alpha3.SkillForAgent{Refs: []string{"example.com/skill:latest"}},
		},
	}

	runtimeInputs := skillsPodRuntime(t, agent)

	assert.Nil(t, runtimeInputs.securityContext.Privileged)
	assertRestrictedSecurityContext(t, runtimeInputs.securityContext)

	require.Len(t, runtimeInputs.initContainers, 1)
	initSecurityContext := runtimeInputs.initContainers[0].SecurityContext
	assert.Nil(t, initSecurityContext.Privileged)
	assertRestrictedSecurityContext(t, initSecurityContext)
}

func TestBuildContainerSecurityContext_CodeExecIsolationKeepsPrivileged(t *testing.T) {
	securityContext := buildContainerSecurityContext(nil, true)

	require.NotNil(t, securityContext)
	require.NotNil(t, securityContext.Privileged)
	assert.True(t, *securityContext.Privileged)
}

func TestBuildContainerSecurityContext_BaseIsPreserved(t *testing.T) {
	base := &corev1.SecurityContext{
		AllowPrivilegeEscalation: new(false),
		ReadOnlyRootFilesystem:   new(true),
	}

	securityContext := buildContainerSecurityContext(base, true)

	require.NotNil(t, securityContext)
	assert.Nil(t, securityContext.Privileged, "an explicit allowPrivilegeEscalation=false suppresses Privileged")
	require.NotNil(t, securityContext.ReadOnlyRootFilesystem)
	assert.True(t, *securityContext.ReadOnlyRootFilesystem)
}
