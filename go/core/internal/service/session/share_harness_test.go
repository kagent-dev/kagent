package session

import (
	"strings"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const shareHarnessSessionID = "11111111-1111-4111-8111-111111111111"

func shareHarnessService(t *testing.T, agent *kagentv1alpha3.Agent, objects ...ctrlclient.Object) (*Service, *serviceTestStore) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := kagentv1alpha3.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	store := &serviceTestStore{getResult: &apiv1alpha1.Session{
		Id:    shareHarnessSessionID,
		Agent: &apiv1alpha1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name},
		State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY,
	}}
	return NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{}, WithAgentReader(kube)), store
}

func shareHarnessAgent(spec kagentv1alpha3.AgentSpec) *kagentv1alpha3.Agent {
	return &kagentv1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "coder"}, Spec: spec}
}

func TestServiceCreateShareRefusedWhenHarnessRunsSenderCode(t *testing.T) {
	for name, spec := range map[string]kagentv1alpha3.AgentSpec{
		"referenced Harness": {HarnessRef: &corev1.LocalObjectReference{Name: "claude"}},
		"inline Harness":     {Harness: &kagentv1alpha3.HarnessSpec{Claude: &kagentv1alpha3.ClaudeHarness{}, RunsSenderCode: true}},
	} {
		t.Run(name, func(t *testing.T) {
			agent := shareHarnessAgent(spec)
			harness := &kagentv1alpha3.Harness{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "claude"},
				Spec:       kagentv1alpha3.HarnessSpec{Claude: &kagentv1alpha3.ClaudeHarness{}, RunsSenderCode: true},
			}
			service, store := shareHarnessService(t, agent, agent, harness)
			for _, permission := range []apiv1alpha1.SessionSharePermission{
				apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_ONLY,
				apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_WRITE,
			} {
				share, token, err := service.CreateShare(serviceTestContext("alice"), shareHarnessSessionID, permission, 0)
				if !serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition) {
					t.Fatalf("CreateShare(%s) error = %v, want FailedPrecondition", permission, err)
				}
				if share != nil || token != "" || store.tokenHash != nil {
					t.Fatalf("refused share of %s returned or stored credentials", permission)
				}
			}
			if store.getID != shareHarnessSessionID || store.getCreator != "alice" {
				t.Fatalf("session lookup = %q of %q, want the caller's session", store.getID, store.getCreator)
			}
		})
	}
}

func TestServiceCreateShareUnchangedWithoutSenderCode(t *testing.T) {
	plain := &kagentv1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "kagent"},
		Spec:       kagentv1alpha3.HarnessSpec{Kagent: &kagentv1alpha3.KagentHarness{}},
	}
	for name, spec := range map[string]kagentv1alpha3.AgentSpec{
		"referenced Harness without the field": {HarnessRef: &corev1.LocalObjectReference{Name: "kagent"}},
		"inline Harness without the field":     {Harness: &kagentv1alpha3.HarnessSpec{Kagent: &kagentv1alpha3.KagentHarness{}}},
	} {
		t.Run(name, func(t *testing.T) {
			agent := shareHarnessAgent(spec)
			service, store := shareHarnessService(t, agent, agent, plain)
			share, token, err := service.CreateShare(serviceTestContext("alice"), shareHarnessSessionID,
				apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_WRITE, 0)
			if err != nil {
				t.Fatal(err)
			}
			if share.GetSessionId() != shareHarnessSessionID || token == "" || store.shareUserID != "alice" {
				t.Fatalf("share = %+v (token %t) of %q, want a share of the caller's session", share, token != "", store.shareUserID)
			}
		})
	}
}

func TestServiceCreateShareRefusedWhenAgentMissing(t *testing.T) {
	service, store := shareHarnessService(t, shareHarnessAgent(kagentv1alpha3.AgentSpec{}))
	_, _, err := service.CreateShare(serviceTestContext("alice"), shareHarnessSessionID,
		apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_ONLY, 0)
	if !serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition) {
		t.Fatalf("CreateShare error = %v, want FailedPrecondition", err)
	}
	if store.tokenHash != nil {
		t.Fatal("a share of a session whose Agent is gone was stored")
	}
}

func TestServiceCreateShareRefusalNamesRemedyOnly(t *testing.T) {
	lower := strings.ToLower(errShareRunsSenderCode)
	if !strings.Contains(lower, "start a session of their own") {
		t.Fatalf("refusal %q names no remedy", errShareRunsSenderCode)
	}
	for _, detail := range []string{"harness", "claude", "kubernetes", "namespace", "runssendercode", "gateway", "slack", "egress", "token"} {
		if strings.Contains(lower, detail) {
			t.Fatalf("refusal %q carries the detail %q", errShareRunsSenderCode, detail)
		}
	}
}
