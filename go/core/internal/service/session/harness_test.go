package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const harnessGoneSessionID = "22222222-2222-4222-8222-222222222222"

// harnessGoneStore grants every dispatch, so a send that passes resolution is
// accepted; the task it names belongs to the fixture's session.
type harnessGoneStore struct {
	interactionTestStore
	reserved int
}

func (s *harnessGoneStore) ReserveSessionDispatch(context.Context, string, uuid.UUID, string) error {
	s.reserved++
	return nil
}

type harnessGoneAgents struct{ kube ctrlclient.Reader }

func (a harnessGoneAgents) Get(ctx context.Context, ref types.NamespacedName) (*kagentv1alpha3.Agent, error) {
	agent := &kagentv1alpha3.Agent{}
	return agent, a.kube.Get(ctx, ref, agent)
}

type harnessGoneFixture struct {
	kube         ctrlclient.Client
	service      *Service
	interactions *InteractionService
	store        *harnessGoneStore
	agent        types.NamespacedName
	harness      *kagentv1alpha3.Harness
}

func newHarnessGoneFixture(t *testing.T) *harnessGoneFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := kagentv1alpha3.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	agent := &kagentv1alpha3.Agent{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "coder"},
		Spec:       kagentv1alpha3.AgentSpec{HarnessRef: &corev1.LocalObjectReference{Name: "claude"}},
	}
	harness := &kagentv1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "claude"},
		Spec:       kagentv1alpha3.HarnessSpec{Claude: &kagentv1alpha3.ClaudeHarness{}},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent, harness).Build()
	sessions := &serviceTestStore{getResult: &apiv1alpha1.Session{
		Id: harnessGoneSessionID, ContextId: harnessGoneSessionID,
		Agent: &apiv1alpha1.ResourceReference{Namespace: agent.Namespace, Name: agent.Name},
		State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, Operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE,
	}}
	service := NewService(sessions, serviceTestAuthorizer{}, serviceTestWorkflow{}, WithAgentReader(kube))
	store := &harnessGoneStore{interactionTestStore: interactionTestStore{task: &a2atype.Task{ID: "task-1", ContextID: harnessGoneSessionID}}}
	return &harnessGoneFixture{
		kube: kube, service: service, store: store,
		interactions: NewInteractionService(store, harnessGoneAgents{kube: kube}, service),
		agent:        types.NamespacedName{Namespace: agent.Namespace, Name: agent.Name},
		harness:      harness,
	}
}

func (f *harnessGoneFixture) create(t *testing.T) error {
	t.Helper()
	_, err := f.service.Create(serviceTestContext("alice"), CreateRequest{
		Agent: &apiv1alpha1.ResourceReference{Namespace: f.agent.Namespace, Name: f.agent.Name}, RequestID: uuid.NewString(),
	})
	return err
}

func (f *harnessGoneFixture) send(t *testing.T, taskID a2atype.TaskID) error {
	t.Helper()
	_, err := f.interactions.PrepareSend(serviceTestContext("alice"), f.agent, &a2atype.SendMessageRequest{
		Message: &a2atype.Message{ID: uuid.NewString(), ContextID: harnessGoneSessionID, TaskID: taskID, Role: a2atype.MessageRoleUser},
	})
	return err
}

// Removing an Agent's Harness switches its runtime off: no new conversation
// and no new turn start on the Agent's last prepared revision, a running turn
// may finish, and restoring the Harness lets new work start again unaided.
func TestHarnessGoneRefusesNewSessionsAndTurns(t *testing.T) {
	f := newHarnessGoneFixture(t)
	if err := f.create(t); err != nil {
		t.Fatalf("Create with the Harness present: %v", err)
	}
	if err := f.send(t, ""); err != nil {
		t.Fatalf("new turn with the Harness present: %v", err)
	}

	if err := f.kube.Delete(t.Context(), f.harness.DeepCopy()); err != nil {
		t.Fatal(err)
	}
	err := f.create(t)
	if !serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition) {
		t.Fatalf("Create without the Harness error = %v, want FailedPrecondition", err)
	}
	for _, detail := range []string{"team/coder", `"team/claude"`, "does not resolve", "ReferenceResolutionFailed"} {
		if !strings.Contains(serviceerrors.MessageOf(err), detail) {
			t.Fatalf("Create refusal %q does not name %q", serviceerrors.MessageOf(err), detail)
		}
	}
	reserved := f.store.reserved
	err = f.send(t, "")
	if !errors.Is(err, a2atype.ErrUnsupportedOperation) || !strings.Contains(err.Error(), `"team/claude"`) {
		t.Fatalf("new turn without the Harness error = %v, want an unsupported operation naming the Harness", err)
	}
	if f.store.reserved != reserved {
		t.Fatal("a refused turn reserved a dispatch")
	}
	if err := f.send(t, "task-1"); err != nil {
		t.Fatalf("continuing a running turn without the Harness: %v", err)
	}

	restored := f.harness.DeepCopy()
	restored.ResourceVersion = ""
	if err := f.kube.Create(t.Context(), restored); err != nil {
		t.Fatal(err)
	}
	if err := f.create(t); err != nil {
		t.Fatalf("Create after the Harness is restored: %v", err)
	}
	if err := f.send(t, ""); err != nil {
		t.Fatalf("new turn after the Harness is restored: %v", err)
	}
}

// An Agent with its Harness inline has no reference that can stop resolving.
func TestInlineHarnessIsNotRefused(t *testing.T) {
	f := newHarnessGoneFixture(t)
	agent := &kagentv1alpha3.Agent{}
	if err := f.kube.Get(t.Context(), f.agent, agent); err != nil {
		t.Fatal(err)
	}
	agent.Spec.HarnessRef = nil
	agent.Spec.Harness = &kagentv1alpha3.HarnessSpec{Claude: &kagentv1alpha3.ClaudeHarness{}}
	if err := f.kube.Update(t.Context(), agent); err != nil {
		t.Fatal(err)
	}
	if err := f.kube.Delete(t.Context(), f.harness.DeepCopy()); err != nil {
		t.Fatal(err)
	}
	if err := f.create(t); err != nil {
		t.Fatalf("Create of an inline-Harness Agent: %v", err)
	}
	if err := f.send(t, ""); err != nil {
		t.Fatalf("new turn of an inline-Harness Agent: %v", err)
	}
}
