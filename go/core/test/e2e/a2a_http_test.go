package e2e_test

import (
	"context"
	"encoding/json"
	"iter"
	"net"
	"net/http"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestSessionHTTPInteraction(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
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
		for event, err := range client.SendStreamingMessage(ctx, request) {
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
		target := interactionTarget(t)
		modelURL, started := startBlockingInteractionMock(t)
		fixture := newInteractionFixture(t, harness, target, modelURL)
		client, ctx := discoverHTTPAgent(t, fixture)
		request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Wait for cancellation"))}
		request.Message.ContextID = fixture.sessionID
		next, stop := iter.Pull2(client.SendStreamingMessage(ctx, request))
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
