package e2e_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	configloader "github.com/kagent-dev/kagent/go/core/pkg/config"
)

// Keep the parent sequential so controller rollouts cannot overlap other tests.
// Harness subtests run in parallel to share the idle period and expiration sweep.
// The API endpoint must survive pod replacement (NodePort or ingress).
func TestSessionIdleExpiration(t *testing.T) {
	target := interactionTarget(t)
	setSessionExpirationPolicy(t, target, "30s", "5s")
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		fixture := newInteractionFixture(t, harness, target, startInteractionMock(t))
		original, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: fixture.sessionID})
		require.NoError(t, err)
		request := &apiv1.CreateSessionRequest{Agent: original.Session.Agent, RequestId: uuid.NewString()}
		created, err := fixture.sessions.CreateSession(fixture.ctx, request)
		require.NoError(t, err)
		id := created.Session.Id
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(fixture.ctx), time.Minute)
			defer cancel()
			require.NoError(t, deleteIdleSession(ctx, fixture.sessions, id))
		})
		conversation := *fixture
		conversation.sessionID, conversation.contextID = id, created.Session.ContextId
		_, _, task := conversation.send(t, "What is 2+2?")
		require.Equal(t, a2a.TaskStateCompleted, task.Status.State)
		assertActorSuspended(t, &conversation)
		require.Eventually(t, func() bool {
			_, err := fixture.sessions.GetSession(fixture.ctx, &apiv1.GetSessionRequest{SessionId: id})
			return status.Code(err) == codes.NotFound
		}, 2*time.Minute, time.Second, "idle session did not expire")
		for _, message := range []*a2a.Message{
			{ID: uuid.NewString(), ContextID: conversation.contextID},
			{ID: uuid.NewString(), ContextID: uuid.NewString()},
			{ID: uuid.NewString(), TaskID: task.ID},
		} {
			message.Role = a2a.MessageRoleUser
			message.Parts = a2a.ContentParts{a2a.NewTextPart("Continue")}
			continuation, err := pbconv.ToProtoSendMessageRequest(&a2a.SendMessageRequest{Tenant: conversation.tenant, Message: message})
			require.NoError(t, err)
			_, err = fixture.client.SendMessage(fixture.ctx, continuation)
			require.Equal(t, codes.NotFound, status.Code(err), "expired and missing conversations must be distinguishable from authorization failures")
		}
		_, err = fixture.client.GetTask(fixture.ctx, &a2apb.GetTaskRequest{Tenant: conversation.tenant, Id: string(task.ID)})
		require.Equal(t, codes.NotFound, status.Code(err))
		actor, err := findSubstrateActor(fixture.ctx, fixture.system, request.Agent.Namespace, substrate.ActorName(id))
		require.NoError(t, err)
		require.Nil(t, actor, "expiration must remove the Substrate Actor")
		var fresh *apiv1.CreateSessionResponse
		require.Eventually(t, func() bool {
			fresh, err = fixture.sessions.CreateSession(fixture.ctx, request)
			return err == nil && fresh.Session.Id != id
		}, time.Minute, time.Second, "expiration must release the conversation request ID")
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(fixture.ctx), time.Minute)
			defer cancel()
			require.NoError(t, deleteIdleSession(ctx, fixture.sessions, fresh.Session.Id))
		})
		require.NotEqual(t, created.Session.ContextId, fresh.Session.ContextId)
	})
}

func setSessionExpirationPolicy(t *testing.T, target, ttl, pollInterval string) {
	t.Helper()
	kube := interactionKubeClient(t)
	deployments := &appsv1.DeploymentList{}
	require.NoError(t, kube.List(t.Context(), deployments, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
	require.Len(t, deployments.Items, 1)
	deployment := &deployments.Items[0]
	configName := ""
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == "controller-config" && volume.ConfigMap != nil {
			configName = volume.ConfigMap.Name
		}
	}
	require.NotEmpty(t, configName)
	configKey := ctrlclient.ObjectKey{Namespace: deployment.Namespace, Name: configName}
	configMap := &corev1.ConfigMap{}
	require.NoError(t, kube.Get(t.Context(), configKey, configMap))
	original := configMap.Data["controller.yaml"]
	cfg := &configv1.ControllerConfig{}
	require.NoError(t, configloader.Decode([]byte(original), cfg))
	configloader.DefaultController(cfg)
	idle, err := time.ParseDuration(ttl)
	require.NoError(t, err)
	poll, err := time.ParseDuration(pollInterval)
	require.NoError(t, err)
	cfg.Sessions.IdleTtl = durationpb.New(idle)
	cfg.Sessions.ExpirationInterval = durationpb.New(poll)
	require.NoError(t, configloader.Validate(cfg))
	updated, err := protojson.Marshal(cfg)
	require.NoError(t, err)
	key := ctrlclient.ObjectKeyFromObject(deployment)
	apply := func(document string) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cm := &corev1.ConfigMap{}
		require.NoError(t, kube.Get(ctx, configKey, cm))
		cmBase := cm.DeepCopy()
		cm.Data["controller.yaml"] = document
		require.NoError(t, kube.Patch(ctx, cm, ctrlclient.MergeFrom(cmBase)))
		current := &appsv1.Deployment{}
		require.NoError(t, kube.Get(ctx, key, current))
		base := current.DeepCopy()
		if current.Spec.Template.Annotations == nil {
			current.Spec.Template.Annotations = map[string]string{}
		}
		current.Spec.Template.Annotations["checksum/config"] = fmt.Sprintf("%x", sha256.Sum256([]byte(document)))
		require.NoError(t, kube.Patch(ctx, current, ctrlclient.MergeFrom(base)))
		require.NoError(t, wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
			observed := &appsv1.Deployment{}
			if err := kube.Get(ctx, key, observed); err != nil {
				return false, err
			}
			return observed.Status.ObservedGeneration >= current.Generation && observed.Status.UpdatedReplicas == *observed.Spec.Replicas && observed.Status.AvailableReplicas == *observed.Spec.Replicas && observed.Status.Replicas == *observed.Spec.Replicas, nil
		}))
		// Deployment readiness can precede Service/load-balancer routing.
		// Check a fresh API connection after both setup and cleanup rollouts
		// before handing the controller back to the next test.
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		defer func() { require.NoError(t, conn.Close()) }()
		err = waitForControllerAPI(ctx, conn)
		require.NoError(t, err, "controller API did not become reachable after rollout")
	}
	t.Cleanup(func() { apply(original) })
	apply(string(updated))
}
