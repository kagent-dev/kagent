package e2e_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	sandboxapi "github.com/kagent-dev/kagent/go/api/sandbox"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type sandboxFixture struct {
	ctx       context.Context
	client    apiv1alpha1.SandboxServiceClient
	processes guestpb.ProcessServiceClient
	files     guestpb.FileSystemServiceClient
	template  *apiv1alpha1.ResourceReference
	system    apiv1alpha1.SystemServiceClient
}

func newSandboxFixture(t *testing.T) *sandboxFixture {
	t.Helper()
	target := interactionTarget(t)
	image := os.Getenv("KAGENT_E2E_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("KAGENT_E2E_SANDBOX_IMAGE is not set")
	}
	namespace := os.Getenv("KAGENT_E2E_SANDBOX_NAMESPACE")
	if namespace == "" {
		namespace = "kagent"
	}
	pool := os.Getenv("KAGENT_E2E_SANDBOX_WORKER_POOL")
	if pool == "" {
		pool = "kagent-default"
	}
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 6*time.Minute)
	t.Cleanup(cancel)
	ref := &apiv1alpha1.ResourceReference{Namespace: namespace, Name: "scratch-" + uuid.NewString()[:8]}
	value, err := structpb.NewStruct(map[string]any{
		"spec": map[string]any{
			"workload": map[string]any{"image": image},
			"substrate": map[string]any{
				"workerPoolRef":  map[string]any{"name": pool},
				"snapshotPolicy": map[string]any{"location": "s3://ate-snapshots/" + namespace},
			},
		},
	})
	require.NoError(t, err)
	catalog := apiv1alpha1.NewSandboxTemplateServiceClient(conn)
	_, err = catalog.CreateSandboxTemplate(ctx, &apiv1alpha1.CreateSandboxTemplateRequest{
		Ref: ref, Resource: &apiv1alpha1.StructuredObject{ApiVersion: "api.kagent.dev/v1alpha3", Kind: "SandboxTemplate", Value: value},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, err := catalog.DeleteSandboxTemplate(cleanup, &apiv1alpha1.DeleteSandboxTemplateRequest{Ref: ref})
		if status.Code(err) != codes.NotFound {
			require.NoError(t, err)
		}
	})
	return &sandboxFixture{ctx: ctx, client: apiv1alpha1.NewSandboxServiceClient(conn), processes: guestpb.NewProcessServiceClient(conn), files: guestpb.NewFileSystemServiceClient(conn), template: ref, system: apiv1alpha1.NewSystemServiceClient(conn)}
}

func (f *sandboxFixture) create(t *testing.T, ttl time.Duration) *apiv1alpha1.Sandbox {
	t.Helper()
	request := &apiv1alpha1.CreateSandboxRequest{SandboxTemplate: f.template, RequestId: uuid.NewString(), Ttl: durationpb.New(ttl)}
	var instance *apiv1alpha1.Sandbox
	require.Eventually(t, func() bool {
		response, err := f.client.CreateSandbox(f.ctx, request)
		if status.Code(err) == codes.FailedPrecondition {
			return false
		}
		require.NoError(t, err)
		instance = response.GetSandbox()
		return true
	}, 3*time.Minute, time.Second, "SandboxTemplate did not prepare")
	t.Logf("sandbox %s: %s (%s)", instance.Id, instance.State, instance.Operation)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, err := f.client.DeleteSandbox(ctx, &apiv1alpha1.DeleteSandboxRequest{SandboxId: instance.Id})
		require.NoError(t, err)
	})
	return f.wait(t, instance.Id, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY)
}

func (f *sandboxFixture) wait(t *testing.T, id string, state apiv1alpha1.RuntimeState) *apiv1alpha1.Sandbox {
	t.Helper()
	var current *apiv1alpha1.Sandbox
	require.Eventually(t, func() bool {
		response, err := f.client.GetSandbox(f.ctx, &apiv1alpha1.GetSandboxRequest{SandboxId: id})
		require.NoError(t, err)
		current = response.GetSandbox()
		return current.State == state && current.Operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE
	}, 2*time.Minute, time.Second, "sandbox %s did not reach %s", id, state)
	return current
}

func (f *sandboxFixture) guestContext(id string) context.Context {
	return metadata.AppendToOutgoingContext(f.ctx, sandboxapi.IDHeader, id)
}

func (f *sandboxFixture) read(t *testing.T, id, path string) []byte {
	t.Helper()
	stream, err := f.files.ReadFile(f.guestContext(id), &guestpb.ReadFileRequest{Path: path})
	require.NoError(t, err)
	var data []byte
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			return data
		}
		require.NoError(t, err)
		data = append(data, chunk.Data...)
	}
}

func TestSandboxLifecycle(t *testing.T) {
	f := newSandboxFixture(t)
	sandbox := f.create(t, 5*time.Minute)
	id := sandbox.Id
	other := metadata.NewOutgoingContext(f.ctx, metadata.Pairs("x-user-id", "another-owner"))
	_, err := f.client.GetSandbox(other, &apiv1alpha1.GetSandboxRequest{SandboxId: id})
	require.Equal(t, codes.NotFound, status.Code(err))
	listed, err := f.client.ListSandboxes(other, &apiv1alpha1.ListSandboxesRequest{})
	require.NoError(t, err)
	require.Empty(t, listed.Sandboxes)

	data := bytes.Repeat([]byte{0, 1, 2, 255, 'a'}, 65000)
	writer, err := f.files.WriteFile(f.guestContext(id))
	require.NoError(t, err)
	require.NoError(t, writer.Send(&guestpb.WriteFileRequest{Path: "binary.dat", Mode: 0600}))
	for offset := 0; offset < len(data); offset += 64000 {
		require.NoError(t, writer.Send(&guestpb.WriteFileRequest{Chunk: data[offset:min(offset+64000, len(data))]}))
	}
	written, err := writer.CloseAndRecv()
	require.NoError(t, err)
	require.EqualValues(t, len(data), written.BytesWritten)
	require.Equal(t, data, f.read(t, id, "binary.dat"))

	start := &guestpb.StartProcessRequest{
		Command: []string{"sh", "-c", "printf once >> count; printf hello; printf warning >&2"},
	}
	process, err := f.processes.StartProcess(f.guestContext(id), start)
	require.NoError(t, err)
	outputs, err := f.processes.StreamProcessOutputs(f.guestContext(id), &guestpb.StreamProcessOutputsRequest{ProcessId: process.ProcessId, Follow: true})
	require.NoError(t, err)
	var stdout, stderr []byte
	for {
		chunk, err := outputs.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if chunk.Source == guestpb.OutputSource_OUTPUT_SOURCE_STDOUT {
			stdout = append(stdout, chunk.Data...)
		} else {
			stderr = append(stderr, chunk.Data...)
		}
	}
	require.Equal(t, "hello", string(stdout))
	require.Equal(t, "warning", string(stderr))
	finished, err := f.processes.GetProcess(f.guestContext(id), &guestpb.GetProcessRequest{ProcessId: process.ProcessId})
	require.NoError(t, err)
	require.Equal(t, guestpb.ProcessStatus_PROCESS_STATUS_COMPLETED, finished.Status)
	require.Equal(t, "once", string(f.read(t, id, "count")))
	again, err := f.processes.StartProcess(f.guestContext(id), start)
	require.NoError(t, err)
	require.NotEqual(t, process.ProcessId, again.ProcessId)
	require.Eventually(t, func() bool {
		current, err := f.processes.GetProcess(f.guestContext(id), &guestpb.GetProcessRequest{ProcessId: again.ProcessId})
		return err == nil && current.Status == guestpb.ProcessStatus_PROCESS_STATUS_COMPLETED
	}, 10*time.Second, 100*time.Millisecond)
	require.Equal(t, "onceonce", string(f.read(t, id, "count")))

	long, err := f.processes.StartProcess(f.guestContext(id), &guestpb.StartProcessRequest{Command: []string{"sleep", "300"}})
	require.NoError(t, err)
	_, err = f.processes.KillProcess(f.guestContext(id), &guestpb.KillProcessRequest{ProcessId: long.ProcessId})
	require.NoError(t, err)
	_, err = f.processes.StartProcess(f.guestContext(id), &guestpb.StartProcessRequest{Command: []string{"sleep", "300"}})
	require.NoError(t, err)
	_, err = f.client.SuspendSandbox(f.ctx, &apiv1alpha1.SuspendSandboxRequest{SandboxId: id})
	require.NoError(t, err)
	f.wait(t, id, apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED)
	_, err = f.client.ResumeSandbox(f.ctx, &apiv1alpha1.ResumeSandboxRequest{SandboxId: id})
	require.NoError(t, err)
	f.wait(t, id, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY)
	require.Equal(t, data, f.read(t, id, "binary.dat"))
	_, err = f.processes.GetProcess(f.guestContext(id), &guestpb.GetProcessRequest{ProcessId: process.ProcessId})
	require.Equal(t, codes.NotFound, status.Code(err))
	_, err = f.client.DeleteSandbox(f.ctx, &apiv1alpha1.DeleteSandboxRequest{SandboxId: id})
	require.NoError(t, err)
	f.wait(t, id, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED)
}

func TestSandboxMCP(t *testing.T) {
	f := newSandboxFixture(t)
	// Preparation and guest execution are exercised independently of agent invocation.
	sandbox := f.create(t, 5*time.Minute)
	call := func(name string, args map[string]any, out any) {
		t.Helper()
		result := mcpCall(t, mcpEndpoint(t), "tools/call", map[string]any{"name": name, "arguments": args}, false)["result"].(map[string]any)
		require.NotEqual(t, true, result["isError"], "%#v", result)
		require.NoError(t, json.Unmarshal([]byte(mcpResultText(result)), out))
	}
	var created struct {
		ID string `json:"id"`
	}
	request := map[string]any{"namespace": f.template.Namespace, "template": f.template.Name, "request_id": uuid.NewString(), "ttl_seconds": 60}
	call("create_sandbox", request, &created)
	f.wait(t, created.ID, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(f.ctx), time.Minute)
		defer cancel()
		_, err := f.client.DeleteSandbox(ctx, &apiv1alpha1.DeleteSandboxRequest{SandboxId: created.ID})
		require.NoError(t, err)
	})
	var retry struct {
		ID string `json:"id"`
	}
	call("create_sandbox", request, &retry)
	require.Equal(t, created.ID, retry.ID)
	var write struct {
		Bytes int `json:"bytes_written"`
	}
	call("write_sandbox_file", map[string]any{"sandbox_id": sandbox.Id, "path": "mcp.txt", "data_base64": base64.StdEncoding.EncodeToString([]byte("from MCP"))}, &write)
	require.Equal(t, 8, write.Bytes)
	var read struct {
		Data string `json:"data_base64"`
	}
	call("read_sandbox_file", map[string]any{"sandbox_id": sandbox.Id, "path": "mcp.txt"}, &read)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("from MCP")), read.Data)
	var process struct {
		ID string `json:"process_id"`
	}
	call("start_sandbox_process", map[string]any{"sandbox_id": sandbox.Id, "command": []string{"cat", "mcp.txt"}}, &process)
	require.NotEmpty(t, process.ID)
	var deleted struct {
		State string `json:"state"`
	}
	call("delete_sandbox", map[string]any{"sandbox_id": created.ID}, &deleted)
	f.wait(t, created.ID, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED)
}

func TestSandboxExpiration(t *testing.T) {
	f := newSandboxFixture(t)
	sandbox := f.create(t, 15*time.Second)
	f.wait(t, sandbox.Id, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED)
}

func TestSandboxTemplateRevisionRetention(t *testing.T) {
	f := newSandboxFixture(t)
	first := f.create(t, 5*time.Minute)
	kube := interactionKubeClient(t)
	template := &v1alpha3.SandboxTemplate{}
	key := types.NamespacedName{Namespace: f.template.Namespace, Name: f.template.Name}
	require.NoError(t, kube.Get(f.ctx, key, template))
	marker := "prepared-v2"
	template.Spec.Env = []v1alpha3.HarnessEnvVar{{Name: "REVISION_MARKER", Value: &marker}}
	require.NoError(t, kube.Update(f.ctx, template))
	require.Eventually(t, func() bool {
		require.NoError(t, kube.Get(f.ctx, key, template))
		return template.Status.ObservedGeneration == template.Generation && meta.IsStatusConditionTrue(template.Status.Conditions, "Ready")
	}, time.Minute, time.Second)
	second := f.create(t, 5*time.Minute)
	require.NotEqual(t, first.PreparedRevision, second.PreparedRevision)
	current, err := f.client.GetSandbox(f.ctx, &apiv1alpha1.GetSandboxRequest{SandboxId: first.Id})
	require.NoError(t, err)
	require.Equal(t, first.PreparedRevision, current.Sandbox.PreparedRevision)
	// Deleting configuration must not remove the pinned runtime inputs.
	require.NoError(t, kube.Delete(f.ctx, template))
	require.Eventually(t, func() bool {
		return apierrors.IsNotFound(kube.Get(f.ctx, key, &v1alpha3.SandboxTemplate{}))
	}, time.Minute, time.Second)
	_, err = f.client.CreateSandbox(f.ctx, &apiv1alpha1.CreateSandboxRequest{SandboxTemplate: f.template, RequestId: uuid.NewString()})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	for _, instance := range []*apiv1alpha1.Sandbox{first, second} {
		_, err := f.client.SuspendSandbox(f.ctx, &apiv1alpha1.SuspendSandboxRequest{SandboxId: instance.Id})
		require.NoError(t, err)
		f.wait(t, instance.Id, apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED)
		_, err = f.client.ResumeSandbox(f.ctx, &apiv1alpha1.ResumeSandboxRequest{SandboxId: instance.Id})
		require.NoError(t, err)
		f.wait(t, instance.Id, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY)
		started, err := f.processes.StartProcess(f.guestContext(instance.Id), &guestpb.StartProcessRequest{
			Command: []string{"sh", "-c", "printf '%s' \"$REVISION_MARKER\" > marker"},
		})
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			process, err := f.processes.GetProcess(f.guestContext(instance.Id), &guestpb.GetProcessRequest{ProcessId: started.ProcessId})
			require.NoError(t, err)
			return process.Status == guestpb.ProcessStatus_PROCESS_STATUS_COMPLETED
		}, time.Second*10, time.Millisecond*100)
		want := ""
		if instance.Id == second.Id {
			want = marker
		}
		require.Equal(t, want, string(f.read(t, instance.Id, "marker")))
	}
}

// This test requires an endpoint that survives pod replacement, such as a
// NodePort or ingress; kubectl port-forward terminates with the original pod.
func TestSandboxControllerRestart(t *testing.T) {
	f := newSandboxFixture(t)
	instance := f.create(t, 5*time.Minute)
	process, err := f.processes.StartProcess(f.guestContext(instance.Id), &guestpb.StartProcessRequest{
		Command: []string{"sh", "-c", "printf once >> restart-count; sleep 5; printf survived"},
	})
	require.NoError(t, err)
	actor, err := findSubstrateActor(f.ctx, f.system, f.template.Namespace, substrate.ActorName(instance.Id))
	require.NoError(t, err)
	require.NotNil(t, actor)
	kube := interactionKubeClient(t)
	pods := &corev1.PodList{}
	require.NoError(t, kube.List(f.ctx, pods, ctrlclient.InNamespace(f.template.Namespace), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
	require.NotEmpty(t, pods.Items)
	for i := range pods.Items {
		require.NoError(t, kube.Delete(f.ctx, &pods.Items[i]))
	}
	require.Eventually(t, func() bool {
		current := &corev1.PodList{}
		if err := kube.List(f.ctx, current, ctrlclient.InNamespace(f.template.Namespace), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}); err != nil {
			return false
		}
		for _, pod := range current.Items {
			old := false
			for _, previous := range pods.Items {
				old = old || previous.UID == pod.UID
			}
			if old {
				continue
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return true
				}
			}
		}
		return false
	}, 90*time.Second, time.Second, "replacement controller did not become ready")
	require.Eventually(t, func() bool {
		response, err := f.client.GetSandbox(f.ctx, &apiv1alpha1.GetSandboxRequest{SandboxId: instance.Id})
		return err == nil && response.Sandbox.State == apiv1alpha1.RuntimeState_RUNTIME_STATE_READY
	}, 90*time.Second, time.Second)
	after, err := findSubstrateActor(f.ctx, f.system, f.template.Namespace, substrate.ActorName(instance.Id))
	require.NoError(t, err)
	require.Equal(t, actor.GetMetadata().GetUid(), after.GetMetadata().GetUid(), "controller restart must preserve compute")
	finished, err := f.processes.GetProcess(f.guestContext(instance.Id), &guestpb.GetProcessRequest{ProcessId: process.ProcessId})
	require.NoError(t, err)
	require.Equal(t, guestpb.ProcessStatus_PROCESS_STATUS_COMPLETED, finished.Status)
	require.Equal(t, "once", string(f.read(t, instance.Id, "restart-count")))
}
