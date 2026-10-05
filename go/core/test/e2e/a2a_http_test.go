package e2e_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func TestSessionHTTPInteraction(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
		client, ctx := discoverHTTPAgent(t, fixture)
		request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 2+2?"))}
		request.Message.ContextID = fixture.sessionID
		result, err := client.SendMessage(ctx, request)
		require.NoError(t, err)
		task, ok := result.(*a2atype.Task)
		require.True(t, ok)
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
		require.Contains(t, taskText(task), "The answer is 4.")
		// Both transports expose the same durable task and history.
		requireSameHTTPTask(t, task, getTask(t, fixture, task.ID))
		persisted, err := client.GetTask(ctx, &a2atype.GetTaskRequest{ID: task.ID})
		require.NoError(t, err)
		require.Equal(t, task, persisted)

		request.Message = a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 2+2?"))
		request.Message.ContextID = fixture.sessionID
		var streamedID a2atype.TaskID
		var completed bool
		for event, err := range sendHTTPStreamingMessageWithRetry(ctx, client, request) {
			require.NoError(t, err)
			require.Equal(t, fixture.contextID, event.TaskInfo().ContextID)
			streamedID = event.TaskInfo().TaskID
			switch event := event.(type) {
			case *a2atype.Task:
				completed = event.Status.State == a2atype.TaskStateCompleted
			case *a2atype.TaskStatusUpdateEvent:
				completed = event.Status.State == a2atype.TaskStateCompleted
			}
		}
		require.True(t, completed)
		require.NotEqual(t, task.ID, streamedID)
		persisted, err = client.GetTask(ctx, &a2atype.GetTaskRequest{ID: streamedID})
		require.NoError(t, err)
		require.Contains(t, taskText(persisted), "The answer is 4.")
	})
}

func TestSessionHTTPResubscribeAndCancel(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		target := interactionTarget(t)
		modelURL, started := startBlockingInteractionMock(t)
		fixture := newInteractionFixture(t, harness, target, modelURL)
		client, ctx := discoverHTTPAgent(t, fixture)
		request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Wait for cancellation"))}
		request.Message.ContextID = fixture.sessionID
		next, stop := iter.Pull2(sendHTTPStreamingMessageWithRetry(ctx, client, request))
		defer stop()
		first, err, ok := next()
		require.True(t, ok)
		require.NoError(t, err)
		taskID := first.TaskInfo().TaskID
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("runtime did not call the blocking model")
		}

		nextSubscription, stopSubscription := iter.Pull2(client.SubscribeToTask(ctx, &a2atype.SubscribeToTaskRequest{ID: taskID}))
		defer stopSubscription()
		subscribed, err, ok := nextSubscription()
		require.True(t, ok)
		require.NoError(t, err)
		require.Equal(t, taskID, subscribed.TaskInfo().TaskID)
		canceled, err := client.CancelTask(ctx, &a2atype.CancelTaskRequest{ID: taskID})
		require.NoError(t, err)
		require.Equal(t, a2atype.TaskStateCanceled, canceled.Status.State)
		for _, receive := range []func() (a2atype.Event, error, bool){next, nextSubscription} {
			var sawCanceled bool
			for {
				event, err, ok := receive()
				if !ok {
					break
				}
				require.NoError(t, err)
				switch event := event.(type) {
				case *a2atype.Task:
					sawCanceled = event.Status.State == a2atype.TaskStateCanceled
				case *a2atype.TaskStatusUpdateEvent:
					sawCanceled = event.Status.State == a2atype.TaskStateCanceled
				}
			}
			require.True(t, sawCanceled)
		}
		persisted, err := client.GetTask(ctx, &a2atype.GetTaskRequest{ID: taskID})
		require.NoError(t, err)
		require.Equal(t, a2atype.TaskStateCanceled, persisted.Status.State)
		requireSameHTTPTask(t, persisted, getTask(t, fixture, taskID))
	})
}

func requireSameHTTPTask(t *testing.T, expected, actual *a2atype.Task) {
	t.Helper()
	expectedProto, err := pbconv.ToProtoTask(expected)
	require.NoError(t, err)
	actualProto, err := pbconv.ToProtoTask(actual)
	require.NoError(t, err)
	require.True(t, proto.Equal(expectedProto, actualProto), "tasks differ: expected %v, actual %v", expectedProto, actualProto)
}

// Match sendMessageWithRetry's rejection contract for JSON-RPC streams. Once an
// event has arrived, the stream must never be restarted, even on a rejection.
func sendHTTPStreamingMessageWithRetry(ctx context.Context, client *a2aclient.Client, request *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			received := false
			for event, err := range client.SendStreamingMessage(ctx, request) {
				var protocolError *a2atype.Error
				if !received && event == nil && errors.Is(err, a2atype.ErrUnsupportedOperation) && errors.As(err, &protocolError) {
					info := protocolError.ErrorInfo().Value
					metadata, _ := info["metadata"].(map[string]string)
					if info["domain"] == a2atype.ProtocolDomain && metadata["reason"] == "KAGENT_SEND_NOT_ACCEPTED" {
						return false, nil
					}
				}
				received = true
				if !yield(event, err) || err != nil {
					break
				}
			}
			return true, nil
		})
		if err != nil {
			yield(nil, err)
		}
	}
}

func discoverHTTPAgent(t *testing.T, fixture *interactionFixture) (*a2aclient.Client, context.Context) {
	t.Helper()
	target := interactionTarget(t)
	request, err := http.NewRequestWithContext(fixture.ctx, http.MethodGet,
		"http://"+target+"/agents/"+fixture.tenant+a2asrv.WellKnownAgentCardPath, nil)
	require.NoError(t, err)
	request.Header.Set("X-User-Id", "e2e")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var card a2atype.AgentCard
	require.NoError(t, json.NewDecoder(response.Body).Decode(&card))
	require.True(t, card.Capabilities.PushNotifications)
	require.Len(t, card.SupportedInterfaces, 2)
	require.Equal(t, a2atype.TransportProtocolJSONRPC, card.SupportedInterfaces[0].ProtocolBinding)
	require.True(t, strings.HasSuffix(card.SupportedInterfaces[0].URL, "/agents/"+fixture.tenant))
	require.Equal(t, a2atype.TransportProtocolGRPC, card.SupportedInterfaces[1].ProtocolBinding)

	// The card advertises a cluster address. Dial through the test's port-forward
	// while retaining the advertised URL, path, and host on the wire.
	transport := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, target)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	client, err := a2aclient.NewFromCard(fixture.ctx, &card, a2aclient.WithJSONRPCTransport(&http.Client{Transport: transport}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Destroy()) })
	ctx := a2aclient.AttachServiceParams(fixture.ctx, a2aclient.ServiceParams{"x-user-id": {"e2e"}})
	return client, ctx
}

func TestSessionHTTPPushNotifications(t *testing.T) {
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t", streaming), func(t *testing.T) {
				exerciseHTTPPush(t, harness, interactionTarget(t), streaming, "", nil)
			})
		}
	})
}

// Keep this parent sequential: it changes controller replica count and replaces
// the leader. The configured API endpoint must survive pod replacement.
func TestSessionHTTPPushFromNonleader(t *testing.T) {
	target := interactionTarget(t)
	followerTarget, replaceLeader := preparePushLeaderHandoff(t)
	exerciseHTTPPush(t, testHarness{name: "kagent", runtimeLabel: "kagent"}, target, true, followerTarget, replaceLeader)
}

// preparePushLeaderHandoff leaves the test with a ready follower to receive
// registration and a hook that replaces the current leader after disconnect.
func preparePushLeaderHandoff(t *testing.T) (string, func()) {
	t.Helper()
	kube := interactionKubeClient(t)
	require.NoError(t, appsv1.AddToScheme(kube.Scheme()))
	require.NoError(t, coordinationv1.AddToScheme(kube.Scheme()))
	deployments := &appsv1.DeploymentList{}
	require.NoError(t, kube.List(t.Context(), deployments, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
	require.Len(t, deployments.Items, 1)
	deployment := deployments.Items[0]
	replicas := deployment.Spec.Replicas
	scale := func(count *int32) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		current := &appsv1.Deployment{}
		require.NoError(t, kube.Get(ctx, ctrlclient.ObjectKeyFromObject(&deployment), current))
		base := current.DeepCopy()
		current.Spec.Replicas = count
		require.NoError(t, kube.Patch(ctx, current, ctrlclient.MergeFrom(base)))
		require.NoError(t, wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
			if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(&deployment), current); err != nil {
				return false, err
			}
			return current.Status.ReadyReplicas == *count && current.Status.Replicas == *count, nil
		}))
	}
	t.Cleanup(func() { scale(replicas) })
	twoReplicas := int32(2)
	scale(&twoReplicas)
	lease := &coordinationv1.Lease{}
	leaseKey := ctrlclient.ObjectKey{Namespace: "kagent", Name: "0e9f6799.kagent.dev"}
	pods := &corev1.PodList{}
	var follower, leaderPod *corev1.Pod
	var leader string
	// Deployment readiness can precede Lease renewal after a Pod replacement.
	// Wait until the Lease holder names a current, ready controller Pod.
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, leaseKey, lease); err != nil {
			return false, err
		}
		if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
			return false, nil
		}
		leader = *lease.Spec.HolderIdentity
		if err := kube.List(ctx, pods, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}); err != nil {
			return false, err
		}
		leaderPod, follower = nil, nil
		for i := range pods.Items {
			pod := &pods.Items[i]
			if pod.DeletionTimestamp != nil {
				continue
			}
			ready := false
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if !ready {
				continue
			}
			if strings.HasPrefix(leader, pod.Name+"_") {
				leaderPod = pod
			} else {
				follower = pod
			}
		}
		return leaderPod != nil && follower != nil, nil
	})
	require.NoError(t, err, "Lease holder %q did not match a ready controller Pod", leader)
	followerTarget := forwardPushController(t, follower)
	return followerTarget, func() {
		require.NoError(t, kube.Get(t.Context(), leaseKey, lease))
		require.Equal(t, leader, *lease.Spec.HolderIdentity, "registration must have been accepted by a nonleader")
		require.NoError(t, kube.Delete(t.Context(), leaderPod))
		require.Eventually(t, func() bool {
			return kube.Get(t.Context(), leaseKey, lease) == nil && lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity != leader
		}, 90*time.Second, time.Second, "replacement leader did not acquire the lease")
	}
}

func pushJWKSKey(t *testing.T, target string) (string, ed25519.PublicKey) {
	t.Helper()
	response, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + target + "/.well-known/jwks.json")
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var set struct {
		Keys []struct {
			KID string `json:"kid"`
			X   string `json:"x"`
		} `json:"keys"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&set))
	require.Len(t, set.Keys, 1)
	public, err := base64.RawURLEncoding.DecodeString(set.Keys[0].X)
	require.NoError(t, err)
	require.Len(t, public, ed25519.PublicKeySize)
	return set.Keys[0].KID, ed25519.PublicKey(public)
}

// exerciseHTTPPush keeps the model blocked until the observation connection is
// gone. The optional hook replaces the leader before allowing task completion.
func exerciseHTTPPush(t *testing.T, harness testHarness, target string, streaming bool, sendTarget string, afterDisconnect func()) {
	t.Helper()
	modelURL, started, unblock, _ := startScheduledRecoveryModel(t)
	fixture := newInteractionFixture(t, harness, target, modelURL)
	keyID, publicKey := pushJWKSKey(t, target)
	callbacks := make(chan *a2atype.TaskStatusUpdateEvent, 8)
	var callbackURL string
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credential, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !found {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		verified, err := jwt.Parse(credential, func(token *jwt.Token) (any, error) {
			if token.Header["kid"] != keyID {
				return nil, fmt.Errorf("unexpected push signing key")
			}
			return publicKey, nil
		}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithAudience(callbackURL))
		if err != nil || !verified.Valid {
			http.Error(w, "invalid JWT", http.StatusUnauthorized)
			return
		}
		var envelope struct {
			StatusUpdate *a2atype.TaskStatusUpdateEvent `json:"statusUpdate"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil || envelope.StatusUpdate == nil {
			http.Error(w, "invalid A2A callback", http.StatusBadRequest)
			return
		}
		if verified.Claims.(jwt.MapClaims)["taskId"] != string(envelope.StatusUpdate.TaskID) {
			http.Error(w, "wrong task", http.StatusUnauthorized)
			return
		}
		callbacks <- envelope.StatusUpdate
		w.WriteHeader(http.StatusNoContent)
	}))
	require.NoError(t, receiver.Listener.Close())
	var err error
	receiver.Listener, err = net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	receiver.Start()
	t.Cleanup(receiver.Close)
	callbackURL = reachableServerURL(t, receiver.URL, "/callback")
	client, ctx := discoverHTTPAgent(t, fixture)
	if sendTarget != "" {
		pinned, err := a2aclient.NewFromEndpoints(ctx, []*a2atype.AgentInterface{a2atype.NewAgentInterface("http://"+sendTarget+"/agents/"+fixture.tenant, a2atype.TransportProtocolJSONRPC)}, a2aclient.WithJSONRPCTransport(&http.Client{}))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, pinned.Destroy()) })
		client = pinned
	}
	request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 2+2?")), Config: &a2atype.SendMessageConfig{ReturnImmediately: true, PushConfig: &a2atype.PushConfig{URL: callbackURL}}}
	observation, cancel := context.WithCancel(ctx)
	defer cancel()
	var taskID a2atype.TaskID
	var contextID string
	if streaming {
		next, stop := iter.Pull2(sendHTTPStreamingMessageWithRetry(observation, client, request))
		defer stop()
		event, err, ok := next()
		require.True(t, ok)
		require.NoError(t, err)
		taskID, contextID = event.TaskInfo().TaskID, event.TaskInfo().ContextID
		cancel()
		stop()
	} else {
		result, err := client.SendMessage(observation, request)
		require.NoError(t, err)
		task := result.(*a2atype.Task)
		taskID, contextID = task.ID, task.ContextID
		cancel()
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(fixture.ctx), time.Minute)
		defer cancel()
		require.NoError(t, deleteIdleSession(cleanup, fixture.sessions, contextID))
	})
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("model did not receive initial task")
	}
	if afterDisconnect != nil {
		afterDisconnect()
	}
	unblock()
	select {
	case event := <-callbacks:
		require.Equal(t, taskID, event.TaskID)
		require.Equal(t, contextID, event.ContextID)
		require.Equal(t, a2atype.TaskStateCompleted, event.Status.State)
	case <-ctx.Done():
		t.Fatal("callback did not arrive after disconnect")
	}
	task, err := client.GetTask(ctx, &a2atype.GetTaskRequest{ID: taskID})
	require.NoError(t, err)
	require.Contains(t, taskText(task), "The answer is 4.")
}

func forwardPushController(t *testing.T, pod *corev1.Pod) string {
	t.Helper()
	cfg, err := config.GetConfig()
	require.NoError(t, err)
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	require.NoError(t, err)
	endpoint, err := url.Parse(cfg.Host)
	require.NoError(t, err)
	endpoint.Path = "/api/v1/namespaces/" + pod.Namespace + "/pods/" + pod.Name + "/portforward"
	stop, ready := make(chan struct{}), make(chan struct{})
	forwarder, err := portforward.New(spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, endpoint), []string{"0:8083"}, stop, ready, io.Discard, io.Discard)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- forwarder.ForwardPorts() }()
	t.Cleanup(func() { close(stop) })
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("forward controller API: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("controller port-forward did not start")
	}
	ports, err := forwarder.GetPorts()
	require.NoError(t, err)
	require.Len(t, ports, 1)
	return fmt.Sprintf("127.0.0.1:%d", ports[0].Local)
}
