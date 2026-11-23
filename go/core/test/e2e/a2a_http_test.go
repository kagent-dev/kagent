package e2e_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	controllerconfig "github.com/kagent-dev/kagent/go/core/pkg/config"
)

func TestSessionHTTPInteraction(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		fixture := newInteractionFixture(t, harness, interactionTarget(t), startInteractionMock(t))
		client, ctx := discoverHTTPAgent(t, fixture)
		request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("What is 2+2?"))}
		request.Message.ContextID = fixture.sessionID
		result, err := sendHTTPMessageWithRetry(ctx, client, request)
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

func sendHTTPMessageWithRetry(ctx context.Context, client *a2aclient.Client, request *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	var result a2atype.SendMessageResult
	// Bound retries without shortening an accepted request's deadline.
	err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 30*time.Second, true, func(context.Context) (bool, error) {
		var err error
		result, err = client.SendMessage(ctx, request)
		if httpSendNotAccepted(err) {
			return false, nil
		}
		return err == nil, err
	})
	return result, err
}

func httpSendNotAccepted(err error) bool {
	var protocolError *a2atype.Error
	if !errors.Is(err, a2atype.ErrUnsupportedOperation) || !errors.As(err, &protocolError) {
		return false
	}
	info := protocolError.ErrorInfo().Value
	metadata, _ := info["metadata"].(map[string]string)
	return info["domain"] == a2atype.ProtocolDomain && metadata["reason"] == "KAGENT_SEND_NOT_ACCEPTED"
}

// Match sendMessageWithRetry's rejection contract for JSON-RPC streams. Once an
// event has arrived, the stream must never be restarted, even on a rejection.
func sendHTTPStreamingMessageWithRetry(ctx context.Context, client *a2aclient.Client, request *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 30*time.Second, true, func(context.Context) (bool, error) {
			received := false
			for event, err := range client.SendStreamingMessage(ctx, request) {
				if !received && event == nil && httpSendNotAccepted(err) {
					return false, nil
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
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t", streaming), func(t *testing.T) {
				t.Parallel()
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
	leaderKID, leaderKey := pushJWKSKey(t, forwardPushController(t, leaderPod))
	followerKID, followerKey := pushJWKSKey(t, followerTarget)
	require.Equal(t, leaderKID, followerKID, "scaled-up replicas must reuse the signing key")
	require.Equal(t, leaderKey, followerKey)
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
	keys := pushJWKSKeys(t, target)
	if len(keys) == 0 {
		return "", nil
	}
	require.Len(t, keys, 1)
	for id, key := range keys {
		return id, key
	}
	return "", nil
}

func pushJWKSKeys(t *testing.T, target string) map[string]ed25519.PublicKey {
	t.Helper()
	response, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + target + "/.well-known/jwks.json")
	require.NoError(t, err)
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil
	}
	require.Equal(t, http.StatusOK, response.StatusCode)
	var set struct {
		Keys []struct {
			KID string `json:"kid"`
			X   string `json:"x"`
		} `json:"keys"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&set))
	keys := make(map[string]ed25519.PublicKey)
	for _, key := range set.Keys {
		public, err := base64.RawURLEncoding.DecodeString(key.X)
		require.NoError(t, err)
		require.Len(t, public, ed25519.PublicKeySize)
		keys[key.KID] = ed25519.PublicKey(public)
	}
	return keys
}

// Keep this test sequential: it rotates shared signing material. The callback
// caches A before rotation, then a later callback verifies against cached A+B.
func TestSessionHTTPPushSigningKeyReload(t *testing.T) {
	apiTarget := interactionTarget(t)
	kube := interactionKubeClient(t)
	deployments := &appsv1.DeploymentList{}
	require.NoError(t, kube.List(t.Context(), deployments, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
	require.Len(t, deployments.Items, 1)
	deployment := deployments.Items[0]
	var signingSecret, configName string
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.ConfigMap != nil {
			configName = volume.ConfigMap.Name
		}
		if volume.Projected != nil {
			for _, source := range volume.Projected.Sources {
				if source.Secret != nil {
					for _, item := range source.Secret.Items {
						if item.Path == "push-private-key.pem" {
							signingSecret = source.Secret.Name
						}
					}
				}
			}
		}
	}
	if signingSecret == "" {
		t.Skip("requires controller.push.signing.secretName; enabled in CI")
	}
	require.Equal(t, new(int32(1)), deployment.Spec.Replicas, "rotation E2E requires one controller replica")
	pod := waitPushControllerPod(t, t.Context(), kube, &deployment)
	target := forwardPushController(t, pod)
	originalID, _ := pushJWKSKey(t, target)
	configMap := &corev1.ConfigMap{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: deployment.Namespace, Name: configName}, configMap))
	cfg := &configv1.ControllerConfig{}
	require.NoError(t, controllerconfig.Decode([]byte(configMap.Data["controller.yaml"]), cfg))
	controllerconfig.DefaultController(cfg)
	secret := &corev1.Secret{}
	secretKey := ctrlclient.ObjectKey{Namespace: deployment.Namespace, Name: signingSecret}
	require.NoError(t, kube.Get(t.Context(), secretKey, secret))
	original := secret.DeepCopy()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		current := &corev1.Secret{}
		require.NoError(t, kube.Get(ctx, secretKey, current))
		current.Data = original.Data
		require.NoError(t, kube.Update(ctx, current))
		// Restore a single-key fixture before other sequential replica tests run.
		currentPod := waitPushControllerPod(t, ctx, kube, &deployment)
		require.NoError(t, kube.Delete(ctx, currentPod))
		restarted := waitPushControllerPod(t, ctx, kube, &deployment)
		require.NotEqual(t, currentPod.UID, restarted.UID)
	})
	var publishedAt time.Time
	oldID := exerciseHTTPPush(t, testHarness{name: "kagent", runtimeLabel: "kagent"}, apiTarget, true, "", func() {
		_, private, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		der, err := x509.MarshalPKCS8PrivateKey(private)
		require.NoError(t, err)
		secret.Data["private-key.pem"] = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		require.NoError(t, kube.Update(t.Context(), secret))
		require.Eventually(t, func() bool {
			keys := pushJWKSKeys(t, target)
			return len(keys) == 2 && keys[originalID] != nil
		}, 2*time.Minute, time.Second, "running pod must publish the replacement from its Secret volume")
		publishedAt = time.Now()
	})
	require.Equal(t, originalID, oldID, "cached original JWKS must verify the callback during staging")
	// The public configuration controls the wait; the 30s buffer is fixed by the
	// rotation contract. Waiting from observation is conservative by one HTTP poll.
	activateAfter := publishedAt.Add(cfg.GetPush().GetSigningKeyActivationDelay().AsDuration() + 30*time.Second)
	timer := time.NewTimer(max(time.Until(activateAfter), 0))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal("timed out waiting for key activation")
	}
	newID := exerciseHTTPPush(t, testHarness{name: "kagent", runtimeLabel: "kagent"}, apiTarget, false, "", nil)
	require.NotEqual(t, originalID, newID)
	require.Contains(t, pushJWKSKeys(t, target), originalID, "old tokens remain verifiable after activation")
	unchanged := &corev1.Pod{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(pod), unchanged))
	require.Equal(t, pod.UID, unchanged.UID)
	for i := range pod.Status.ContainerStatuses {
		require.Equal(t, pod.Status.ContainerStatuses[i].RestartCount, unchanged.Status.ContainerStatuses[i].RestartCount)
	}
	exercisePushSigningRollout(t, kube, &deployment, configMap, cfg, signingSecret, newID)
}

// Given B is active, when a Secret-name rollout installs C/current and B/previous,
// assert that callbacks use B during staging, then survive a Pod restart with C.
func exercisePushSigningRollout(t *testing.T, kube ctrlclient.Client, deployment *appsv1.Deployment,
	configMap *corev1.ConfigMap, cfg *configv1.ControllerConfig, previousSecret, previousID string,
) {
	t.Helper()
	const (
		currentFile      = "push-private-key.pem"
		previousFile     = "push-previous-private-key.pem"
		secretKey        = "private-key.pem"
		controllerConfig = "controller.yaml"
	)
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(deployment), deployment))
	originalDeployment, originalConfig := deployment.DeepCopy(), configMap.DeepCopy()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	currentSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: deployment.Namespace, GenerateName: "push-signing-rollout-"},
		Data:       map[string][]byte{secretKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})},
	}
	require.NoError(t, kube.Create(t.Context(), currentSecret))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		restoredConfig := &corev1.ConfigMap{}
		require.NoError(t, kube.Get(ctx, ctrlclient.ObjectKeyFromObject(configMap), restoredConfig))
		restoredConfig.Data = originalConfig.Data
		require.NoError(t, kube.Update(ctx, restoredConfig))
		restoredDeployment := &appsv1.Deployment{}
		require.NoError(t, kube.Get(ctx, ctrlclient.ObjectKeyFromObject(deployment), restoredDeployment))
		restoredDeployment.Spec = originalDeployment.Spec
		require.NoError(t, kube.Update(ctx, restoredDeployment))
		waitPushControllerPod(t, ctx, kube, restoredDeployment)
		require.NoError(t, kube.Delete(ctx, currentSecret))
	})
	harness := testHarness{name: "kagent", runtimeLabel: "kagent"}
	apiTarget := interactionTarget(t)
	var pod *corev1.Pod
	var currentID string
	// Prepare a blocked task before rollout, so runtime provisioning does not
	// consume the interval in which the callback must use the previous key.
	id := exerciseHTTPPush(t, harness, apiTarget, false, "", func() {
		// These are the config and projection changes produced by Helm's two Secret values.
		// Leave time for the two-minute rollout bound before testing old-key signing.
		cfg.Push.SigningKeyActivationDelay = durationpb.New(max(cfg.Push.SigningKeyActivationDelay.AsDuration(), 2*time.Minute))
		cfg.Push.PreviousSigningPrivateKeyFile = "/etc/kagent/secrets/" + previousFile
		contents, err := protojson.Marshal(cfg)
		require.NoError(t, err)
		configMap.Data[controllerConfig] = string(contents)
		require.NoError(t, kube.Update(t.Context(), configMap))
		replaced := false
		for i := range deployment.Spec.Template.Spec.Volumes {
			projection := deployment.Spec.Template.Spec.Volumes[i].Projected
			if projection == nil {
				continue
			}
			for j := range projection.Sources {
				secret := projection.Sources[j].Secret
				if secret == nil || !slices.ContainsFunc(secret.Items, func(item corev1.KeyToPath) bool { return item.Path == currentFile }) {
					continue
				}
				secret.Name = currentSecret.Name
				projection.Sources = append(projection.Sources, corev1.VolumeProjection{Secret: &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{Name: previousSecret}, Optional: new(true),
					Items: []corev1.KeyToPath{{Key: secretKey, Path: previousFile}},
				}})
				replaced = true
				break
			}
		}
		require.True(t, replaced)
		require.NoError(t, kube.Update(t.Context(), deployment))
		pod = waitPushControllerPod(t, t.Context(), kube, deployment)
		keys := pushJWKSKeys(t, forwardPushController(t, pod))
		require.Len(t, keys, 2)
		require.Contains(t, keys, previousID)
		for id, public := range keys {
			if id != previousID {
				require.Equal(t, private.Public(), public)
				currentID = id
			}
		}
	})
	require.Equal(t, previousID, id, "routine Secret-name rollout must preserve previous-key signing")
	// Give the replacement Pod its full activation delay before checking C.
	timer := time.NewTimer(cfg.GetPush().GetSigningKeyActivationDelay().AsDuration() + 30*time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal("timed out waiting for rollout key activation")
	}
	id = exerciseHTTPPush(t, harness, apiTarget, false, "", nil)
	require.Equal(t, currentID, id)
	require.NoError(t, kube.Delete(t.Context(), pod))
	restarted := waitPushControllerPod(t, t.Context(), kube, deployment)
	require.NotEqual(t, pod.UID, restarted.UID)
	keys := pushJWKSKeys(t, forwardPushController(t, restarted))
	require.Contains(t, keys, previousID)
	require.Contains(t, keys, currentID)
	id = exerciseHTTPPush(t, harness, apiTarget, false, "", nil)
	require.Equal(t, currentID, id, "restarting the controller must preserve current-key signing")
}

func waitPushControllerPod(t *testing.T, ctx context.Context, kube ctrlclient.Client, deployment *appsv1.Deployment) *corev1.Pod {
	t.Helper()
	var ready *corev1.Pod
	require.Eventually(t, func() bool {
		current := &appsv1.Deployment{}
		if kube.Get(ctx, ctrlclient.ObjectKeyFromObject(deployment), current) != nil ||
			current.Status.ObservedGeneration < current.Generation || current.Status.UpdatedReplicas != 1 || current.Status.Replicas != 1 {
			return false
		}
		pods := &corev1.PodList{}
		if kube.List(ctx, pods, ctrlclient.InNamespace(deployment.Namespace), ctrlclient.MatchingLabels(deployment.Spec.Selector.MatchLabels)) != nil || len(pods.Items) != 1 {
			return false
		}
		pod := &pods.Items[0]
		if pod.DeletionTimestamp != nil || !slices.ContainsFunc(pod.Status.Conditions, func(c corev1.PodCondition) bool {
			return c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue
		}) {
			return false
		}
		ready = pod
		return true
	}, 2*time.Minute, time.Second, "controller Pod must finish rollout")
	return ready
}

// exerciseHTTPPush keeps the model blocked until the observation connection is
// gone. The optional hook replaces the leader before allowing task completion.
func exerciseHTTPPush(t *testing.T, harness testHarness, target string, streaming bool, sendTarget string, afterDisconnect func()) string {
	t.Helper()
	modelURL, started, unblock, _ := startScheduledRecoveryModel(t)
	fixture := newInteractionFixture(t, harness, target, modelURL)
	var contextID string
	defer func() {
		// Release the model even on assertion failure. A completed callback can
		// precede automatic suspension, so clean up before callers restart Pods.
		unblock()
		if contextID == "" {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(fixture.ctx), time.Minute)
		defer cancel()
		require.NoError(t, deleteIdleSession(cleanup, fixture.sessions, contextID), "delete push session %s", contextID)
	}()
	publicKeys := pushJWKSKeys(t, target)
	signingKeys := make(chan string, 8)
	callbacks := make(chan *a2atype.TaskStatusUpdateEvent, 8)
	var callbackURL string
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var verified *jwt.Token
		if len(publicKeys) != 0 {
			credential, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !found {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var err error
			verified, err = jwt.Parse(credential, func(token *jwt.Token) (any, error) {
				id, _ := token.Header["kid"].(string)
				publicKey, found := publicKeys[id]
				if !found {
					return nil, fmt.Errorf("unexpected push signing key")
				}
				return publicKey, nil
			}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithAudience(callbackURL))
			if err != nil || !verified.Valid {
				http.Error(w, "invalid JWT", http.StatusUnauthorized)
				return
			}
		} else if r.Header.Get("Authorization") != "" {
			http.Error(w, "unexpected authentication on unsigned callback", http.StatusUnauthorized)
			return
		}
		var envelope struct {
			StatusUpdate *a2atype.TaskStatusUpdateEvent `json:"statusUpdate"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil || envelope.StatusUpdate == nil {
			http.Error(w, "invalid A2A callback", http.StatusBadRequest)
			return
		}
		if verified != nil && verified.Claims.(jwt.MapClaims)["taskId"] != string(envelope.StatusUpdate.TaskID) {
			http.Error(w, "wrong task", http.StatusUnauthorized)
			return
		}
		if verified != nil {
			signingKeys <- verified.Header["kid"].(string)
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
		result, err := sendHTTPMessageWithRetry(observation, client, request)
		require.NoError(t, err)
		task := result.(*a2atype.Task)
		taskID, contextID = task.ID, task.ContextID
		cancel()
	}
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
	if len(publicKeys) != 0 {
		return <-signingKeys
	}
	return ""
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
