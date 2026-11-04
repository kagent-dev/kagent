package anthropicselfhostedsandboxes

import (
	"context"
	"errors"
	"testing"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

func TestDiscoveryRestoresPagesAndKeepsLiveWorkers(t *testing.T) {
	live, idle, unrelated := readySandbox(), readySandbox(), readySandbox()
	idle.Id = "00000000-0000-4000-8000-000000000002"
	idle.Name = sandboxNamePrefix + "session_idle"
	unrelated.Name = "another-purpose"
	pages, scans, suspends := 0, 0, 0
	fixture := &sandboxFixture{
		list: func(_ context.Context, req *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error) {
			assert.Equal(t, testConfig().TemplateName, req.GetSandboxTemplate().GetName())
			assert.Equal(t, testConfig().TemplateNamespace, req.GetSandboxTemplate().GetNamespace())
			pages++
			if req.GetPage().GetPageToken() == "" {
				return &apiv1.ListSandboxesResponse{Sandboxes: []*apiv1.Sandbox{live, unrelated},
					Page: &apiv1.PageResponse{NextPageToken: "next"}}, nil
			}
			return &apiv1.ListSandboxesResponse{Sandboxes: []*apiv1.Sandbox{idle}}, nil
		},
		lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
			if id == live.Id {
				return live, nil
			}
			return idle, nil
		},
		start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			assert.Equal(t, 2, pages, "finish discovery before inspecting guests")
			assert.Equal(t, []string{"ps", "-eo", "comm="}, req.Command)
			scans++
			return &guestpb.Process{ProcessId: "ps"}, nil
		},
		output: func(_ context.Context, id string, req *guestpb.StreamProcessOutputRequest, send func(*guestpb.ProcessOutput) error) error {
			assert.True(t, req.Follow)
			output := "guest-server\nps\n"
			if id == live.Id {
				output += "ant\n"
			}
			// Deliberately split inside a command name.
			for _, part := range []string{output[:len(output)-2], output[len(output)-2:]} {
				if err := send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Stdout{Stdout: []byte(part)}}); err != nil {
					return err
				}
			}
			return send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Exit{Exit: &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}}})
		},
		suspend: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
			assert.Equal(t, idle.Id, id, "never suspend the discovered live CLI")
			suspends++
			idle.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
			return idle, nil
		},
	}
	launcher := newTestLauncher(t, fixture, &workAPI{})
	require.NoError(t, launcher.adopt(t.Context()))
	assert.Len(t, launcher.processes, 2)
	assert.Equal(t, live.Id, launcher.processes["session_test"].sandboxID)
	assert.Equal(t, idle.Id, launcher.processes["session_idle"].sandboxID)
	assert.Equal(t, 2, scans)
	assert.Equal(t, 1, suspends)
}

func TestDiscoveredSandboxResumesWithoutRecreation(t *testing.T) {
	instance := readySandbox()
	instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
	creates, resumes, starts := 0, 0, 0
	fixture := &sandboxFixture{
		list: func(context.Context, *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error) {
			return &apiv1.ListSandboxesResponse{Sandboxes: []*apiv1.Sandbox{instance}}, nil
		},
		lookup: func(context.Context, string) (*apiv1.Sandbox, error) { return instance, nil },
		create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			creates++
			return nil, errors.New("unexpected create")
		},
		resume: func(context.Context, string) (*apiv1.Sandbox, error) {
			resumes++
			instance.State = apiv1.RuntimeState_RUNTIME_STATE_READY
			return instance, nil
		},
		start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			assert.Equal(t, "ant", req.Command[0])
			starts++
			return &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
		scan: func(context.Context, string) (string, error) {
			return "ant\n", nil
		},
	}
	launcher := newTestLauncher(t, fixture, &workAPI{})
	require.NoError(t, launcher.adopt(t.Context()))
	require.NoError(t, launcher.launch(t.Context(), testWork("new")))
	require.NoError(t, launcher.launch(t.Context(), testWork("new")))
	assert.Zero(t, creates)
	assert.Equal(t, 1, resumes)
	assert.Equal(t, 1, starts, "duplicate delivery must not start another CLI")
}

func TestProcessDiscoveryFailsClosed(t *testing.T) {
	for _, phase := range []string{"start", "missing process ID", "stream", "stream not found", "missing exit", "failed ps", "unknown exit"} {
		t.Run(phase, func(t *testing.T) {
			launcher := newTestLauncher(t, &sandboxFixture{
				start: func(ctx context.Context, id string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					assert.Equal(t, testSandboxID, id)
					assert.Equal(t, []string{"ps", "-eo", "comm="}, req.Command)
					assert.Equal(t, collectionTimeout, req.Timeout.AsDuration())
					_, bounded := ctx.Deadline()
					assert.True(t, bounded)
					if phase == "start" {
						return nil, serviceerrors.NewUnavailable("secret-body", nil)
					}
					if phase == "missing process ID" {
						return &guestpb.Process{}, nil
					}
					return &guestpb.Process{ProcessId: "ps"}, nil
				},
				output: func(_ context.Context, _ string, _ *guestpb.StreamProcessOutputRequest, send func(*guestpb.ProcessOutput) error) error {
					if phase == "stream" {
						return errors.New("secret-body")
					}
					if phase == "stream not found" {
						return serviceerrors.NewNotFound("secret-body", nil)
					}
					if phase == "missing exit" {
						return nil
					}
					process := &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}
					if phase == "failed ps" {
						process.ExitCode = 127
					}
					if phase == "unknown exit" {
						process.State = guestpb.ProcessState_PROCESS_STATE_UNSPECIFIED
					}
					return send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Exit{Exit: process}})
				},
				suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
					t.Error("failed process discovery must not suspend a sandbox")
					return nil, errors.New("unexpected suspend")
				},
			}, &workAPI{})
			trackTestProcesses(t, launcher, "worker")
			err := launcher.collectSandbox(t.Context(), "session_test")
			require.Error(t, err)
			assert.Equal(t, "work_worker", launcher.processes["session_test"].workID)
			assert.Equal(t, testSandboxID, launcher.processes["session_test"].sandboxID)
			assert.NotContains(t, err.Error(), "secret-body")
		})
	}
}

func TestDiscoveredWorkerBlocksReplacement(t *testing.T) {
	starts := 0
	api := &workAPI{}
	launcher := newTestLauncher(t, &sandboxFixture{
		list: func(context.Context, *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error) {
			return &apiv1.ListSandboxesResponse{Sandboxes: []*apiv1.Sandbox{readySandbox()}}, nil
		},
		start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			if req.Command[0] == "ant" {
				starts++
			}
			return &guestpb.Process{ProcessId: "ps"}, nil
		},
		output: func(_ context.Context, _ string, _ *guestpb.StreamProcessOutputRequest, send func(*guestpb.ProcessOutput) error) error {
			if err := send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Stdout{Stdout: []byte("ant\n")}}); err != nil {
				return err
			}
			return send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Exit{Exit: &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}}})
		},
		suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
			t.Error("must not suspend a discovered live CLI")
			return nil, errors.New("unexpected suspension")
		},
	}, api)
	require.NoError(t, launcher.adopt(t.Context()))
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	require.Error(t, launcher.launch(ctx, testWork("incoming")))
	assert.Zero(t, starts)
	assert.Zero(t, api.stops, "uncertain handoff must leave the incoming lease to expire")
}

func TestDiscoveryRejectsConflictingActiveGenerations(t *testing.T) {
	first, second := readySandbox(), readySandbox()
	second.Id = "00000000-0000-4000-8000-000000000002"
	launcher := newTestLauncher(t, &sandboxFixture{
		list: func(context.Context, *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error) {
			return &apiv1.ListSandboxesResponse{Sandboxes: []*apiv1.Sandbox{first, second}}, nil
		},
	}, &workAPI{})
	require.ErrorContains(t, launcher.adopt(t.Context()), "conflicting")
	assert.Empty(t, launcher.processes)
}

func TestDiscoverySelectsNewestExpiredGeneration(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "old first", true: "latest first"}[reverse], func(t *testing.T) {
			old, latest := readySandbox(), readySandbox()
			old.CreatedAt = timestamppb.New(time.Now().Add(-12 * time.Hour))
			old.ExpiresAt = timestamppb.New(time.Now().Add(-2 * time.Hour))
			latest.Id = "00000000-0000-4000-8000-000000000002"
			latest.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
			latest.CreatedAt = timestamppb.New(time.Now().Add(-time.Hour))
			latest.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
			instances := []*apiv1.Sandbox{old, latest}
			if reverse {
				instances[0], instances[1] = instances[1], instances[0]
			}
			launcher := newTestLauncher(t, &sandboxFixture{
				list: func(context.Context, *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error) {
					return &apiv1.ListSandboxesResponse{Sandboxes: instances}, nil
				},
				lookup: func(context.Context, string) (*apiv1.Sandbox, error) { return latest, nil },
			}, &workAPI{})
			require.NoError(t, launcher.adopt(t.Context()))
			assert.Equal(t, latest.Id, launcher.processes["session_test"].sandboxID)
		})
	}
}
