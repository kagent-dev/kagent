package managedagents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

func metadataWork(t *testing.T, workID, processID string) *anthropic.BetaSelfHostedWork {
	t.Helper()
	work := testWork(workID)
	work.Secret = "" // Non-poll retrieval never supplies worker credentials.
	work.State = anthropic.BetaSelfHostedWorkStateStopped
	work.Metadata = map[string]string{managedByKey: managedByValue, sandboxIDKey: testSandboxID, processIDKey: processID, "unrelated": "preserved"}
	return work
}

func writeWorkPage(t *testing.T, w http.ResponseWriter, works []*anthropic.BetaSelfHostedWork, next string) {
	t.Helper()
	require.NoError(t, json.NewEncoder(w).Encode(struct {
		Data     []*anthropic.BetaSelfHostedWork `json:"data"`
		NextPage string                          `json:"next_page"`
	}{Data: works, NextPage: next}))
}

func TestMetadataHandoffReplacesExitedProcess(t *testing.T) {
	var starts int
	api := &workAPI{}
	launcher := newTestLauncher(t, &sandboxFixture{
		start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			starts++
			id := "proc-z"
			if starts == 2 {
				id = "proc-a"
			}
			return &guestpb.Process{ProcessId: id, State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
	}, api)
	api.heartbeat = func(w http.ResponseWriter, _ *http.Request, _ int) {
		_, _ = io.WriteString(w, `{"last_heartbeat":"cli-token","ttl_seconds":60,"lease_extended":true,"state":"active"}`)
	}
	for range 2 {
		require.NoError(t, launcher.launch(t.Context(), testWork("work_test")))
	}
	assert.Equal(t, "proc-a", api.metadata["work_test"][processIDKey])
	assert.Equal(t, "proc-a", launcher.processes["session_test"].processID)
	assert.Equal(t, "work_test", launcher.processes["session_test"].workID)
	assert.Equal(t, testSandboxID, api.metadata["work_test"][sandboxIDKey])
	assert.Equal(t, managedByValue, api.metadata["work_test"][managedByKey])
	assert.NotContains(t, fmt.Sprint(api.metadata), "cli-token")
	assert.NotContains(t, fmt.Sprint(api.metadata), testWork("").Secret)
}

func TestSuccessfulHandoffPublishesAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	api := &workAPI{queue: []*anthropic.BetaSelfHostedWork{testWork("work_test")}}
	launcher := newTestLauncher(t, &sandboxFixture{
		start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			cancel()
			return &guestpb.Process{ProcessId: "proc-success", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
		suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
			t.Error("controller shutdown must not suspend workers")
			return nil, errors.New("unexpected suspend")
		},
	}, api)
	require.NoError(t, launcher.Start(ctx))
	assert.Equal(t, 3, api.updates)
	assert.Equal(t, "proc-success", api.metadata["work_test"][processIDKey])
	assert.Zero(t, api.stops)
	require.Contains(t, launcher.processes, "session_test")
}

func TestCanceledStartLeavesWorkLeaseToExpire(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	api := &workAPI{}
	launcher := newTestLauncher(t, &sandboxFixture{
		start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			cancel()
			return nil, context.Canceled
		},
	}, api)
	require.Error(t, launcher.launch(ctx, testWork("work_test")))
	assert.Zero(t, api.stops)
	assert.Equal(t, 2, api.updates)
	requireIdleSandbox(t, launcher, "session_test")
}

func TestMetadataRetriesStayWithLaunchAndSuspend(t *testing.T) {
	var processWrites, cleanupWrites atomic.Int32
	var exited bool
	gets, suspends := 0, 0
	api := &workAPI{update: func(w http.ResponseWriter, _ *http.Request, values map[string]*string) {
		failed := values[processIDKey] != nil && processWrites.Add(1) == 1
		if _, cleanup := values[processIDKey]; cleanup && values[processIDKey] == nil && values[sandboxIDKey] == nil {
			failed = cleanupWrites.Add(1) == 1
		}
		if failed {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"standing-environment-secret opaque-scoped-work-secret"}}`)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}}
	launcher := newTestLauncher(t, &sandboxFixture{
		get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
			gets++
			state := guestpb.ProcessState_PROCESS_STATE_RUNNING
			if exited {
				state = guestpb.ProcessState_PROCESS_STATE_EXITED
			}
			return &guestpb.Process{State: state}, nil
		},
		suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
			suspends++
			return suspendedSandbox(), nil
		},
	}, api)
	var logs bytes.Buffer
	launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
	require.NoError(t, launcher.launch(t.Context(), testWork("work_test")))
	assert.EqualValues(t, 2, processWrites.Load(), "launch must retry its own process publication")
	assert.Equal(t, "process-test", api.metadata["work_test"][processIDKey])
	assert.Zero(t, suspends)
	assert.Zero(t, api.stops)
	writes := api.updates
	launcher.collect(t.Context())
	assert.Equal(t, writes, api.updates, "GC must not publish metadata for a running CLI")

	exited = true
	launcher.collect(t.Context())
	assert.EqualValues(t, 2, cleanupWrites.Load(), "suspension must retry its own null patch")
	requireIdleSandbox(t, launcher, "session_test")
	assert.Equal(t, 2, gets)
	assert.Equal(t, 1, suspends)
	assert.NotContains(t, api.metadata["work_test"], processIDKey)
	assert.Equal(t, testSandboxID, api.metadata["work_test"][sandboxIDKey])
	assert.Equal(t, managedByValue, api.metadata["work_test"][managedByKey])
	assert.NotContains(t, logs.String(), testConfig().EnvironmentKey)
	assert.NotContains(t, logs.String(), testWork("").Secret)
}

func TestCollectorDoesNotRetryIncompleteMetadata(t *testing.T) {
	for _, phase := range []string{"launch", "suspend"} {
		t.Run(phase, func(t *testing.T) {
			fail := true
			suspended := false
			starts := 0
			api := &workAPI{update: func(w http.ResponseWriter, _ *http.Request, values map[string]*string) {
				processWrite := values[processIDKey] != nil
				_, hasProcess := values[processIDKey]
				cleanup := hasProcess && !processWrite && values[sandboxIDKey] == nil
				if fail && ((phase == "launch" && processWrite) || (phase == "suspend" && cleanup)) {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = io.WriteString(w, `{}`)
			}}
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(context.Context, string) (*apiv1.Sandbox, error) {
					instance := readySandbox()
					if suspended {
						instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
					}
					return instance, nil
				},
				get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					state := guestpb.ProcessState_PROCESS_STATE_RUNNING
					if phase == "suspend" {
						state = guestpb.ProcessState_PROCESS_STATE_EXITED
					}
					return &guestpb.Process{State: state}, nil
				},
				start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					starts++
					return &guestpb.Process{ProcessId: "process-test", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
				suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
					suspended = true
					return suspendedSandbox(), nil
				},
				resume: func(context.Context, string) (*apiv1.Sandbox, error) {
					suspended = false
					return readySandbox(), nil
				},
			}, api)
			ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
			defer cancel()
			if phase == "launch" {
				err := launcher.launch(ctx, testWork("work_test"))
				require.ErrorContains(t, err, "CLI is running")
				assert.NotContains(t, api.metadata["work_test"], processIDKey)
			} else {
				require.NoError(t, launcher.launch(t.Context(), testWork("work_test")))
				require.Error(t, launcher.collectSandbox(ctx, "session_test"))
				assert.Equal(t, "process-test", api.metadata["work_test"][processIDKey])
			}
			entry := launcher.processes["session_test"]
			if phase == "launch" {
				require.NotNil(t, entry)
			} else {
				requireIdleSandbox(t, launcher, "session_test")
			}
			assert.Zero(t, api.stops, "a metadata timeout must leave the established CLI alone")
			writes := api.updates
			fail = false
			launcher.collect(t.Context())
			assert.Equal(t, writes, api.updates, "GC must not flush timed-out metadata operations")
			if phase == "launch" {
				assert.Equal(t, "work_test", entry.workID, "GC must retain the running worker")
			}

			next := "work_test"
			if phase == "suspend" {
				next = "work_next"
			}
			require.NoError(t, launcher.launch(t.Context(), testWork(next)))
			assert.Equal(t, launcher.processes["session_test"].processID, api.metadata[next][processIDKey])
			if phase == "launch" {
				assert.Equal(t, 1, starts, "an explicit launch retry must reuse the live CLI")
			} else {
				assert.Equal(t, 2, starts)
				assert.NotContains(t, api.metadata["work_test"], processIDKey, "handoff must finish previous suspension cleanup")
			}
		})
	}
}

func TestAdoptionRestoresAllPagesBeforePolling(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var gets atomic.Int32
	inspected := make(chan struct{})
	markInspected := sync.OnceFunc(func() { close(inspected) })
	api := &workAPI{
		queue: []*anthropic.BetaSelfHostedWork{testWork("work_first")},
		list: func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("page") {
			case "":
				writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{metadataWork(t, "work_first", "proc-a")}, "page-two")
			case "page-two":
				work := metadataWork(t, "work_second", "proc-c")
				work.Data.ID = "session_second"
				work.Metadata[sandboxIDKey] = "00000000-0000-4000-8000-000000000002"
				writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{work}, "")
			default:
				t.Error("unexpected page cursor")
			}
		},
		onEmpty: func() {
			select {
			case <-inspected:
				cancel()
			case <-ctx.Done():
			}
		},
	}
	launcher := newTestLauncher(t, &sandboxFixture{
		lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
			instance := readySandbox()
			instance.Id = id
			return instance, nil
		},
		create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			t.Error("adoption must not create or resume a worker")
			return nil, errors.New("unexpected create")
		},
		get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
			api.mu.Lock()
			assert.Equal(t, 2, api.lists, "all adoption pages precede polling and guest access")
			api.mu.Unlock()
			gets.Add(1)
			markInspected()
			return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
	}, api)
	require.NoError(t, launcher.Start(ctx))
	entry := launcher.processes["session_test"]
	require.NotNil(t, entry)
	assert.Equal(t, "proc-a", entry.processID)
	assert.Equal(t, "work_first", entry.workID)
	assert.Equal(t, "proc-c", launcher.processes["session_second"].processID)
	// A polled adopted assignment must never claim the CLI's lease.
	assert.Zero(t, api.beats)
	assert.Zero(t, api.stops)
	assert.Equal(t, 1, api.updates, "explicit duplicate launch republishes its worker identity")
	assert.Equal(t, 2, api.lists)
	assert.EqualValues(t, 3, gets.Load(), "recovery inspects both workers before the adopted assignment")
	launcher.collect(t.Context())
	assert.GreaterOrEqual(t, gets.Load(), int32(3))
}

func TestAdoptedExitedWorkCanLaunchAgain(t *testing.T) {
	api := &workAPI{list: func(w http.ResponseWriter, _ *http.Request) {
		writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{metadataWork(t, "work_test", "old-proc")}, "")
	}}
	var exited bool
	launcher := newTestLauncher(t, &sandboxFixture{get: func(_ context.Context, _ string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
		state := guestpb.ProcessState_PROCESS_STATE_RUNNING
		if exited {
			state = guestpb.ProcessState_PROCESS_STATE_EXITED
		}
		return &guestpb.Process{ProcessId: req.ProcessId, State: state}, nil
	}}, api)
	require.NoError(t, launcher.adopt(t.Context()))
	exited = true
	require.NoError(t, launcher.launch(t.Context(), testWork("work_test")))
	assert.Equal(t, 1, api.beats)
	assert.Equal(t, "process-test", api.metadata["work_test"][processIDKey])
	assert.Equal(t, "process-test", launcher.processes["session_test"].processID)
	assert.Zero(t, api.stops)
}

func TestAdoptionSkipsConflictingRecords(t *testing.T) {
	for _, conflict := range []string{"generation", "session", "process owner", "worker"} {
		t.Run(conflict, func(t *testing.T) {
			first, second := metadataWork(t, "work_first", "proc-a"), metadataWork(t, "work_second", "proc-b")
			switch conflict {
			case "generation":
				second.Metadata[sandboxIDKey] = "c35730fc-1462-46c9-9298-3f759cad9aff"
			case "session":
				second.Data.ID = "session_other"
			case "process owner":
				second = metadataWork(t, "work_second", "proc-a")
			}
			api := &workAPI{list: func(w http.ResponseWriter, _ *http.Request) {
				writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{first, second}, "")
			}}
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
					instance := readySandbox()
					instance.Id = id
					return instance, nil
				},
				get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
			}, api)
			var logs bytes.Buffer
			launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
			require.NoError(t, launcher.adopt(t.Context()))
			assert.Len(t, launcher.processes, 1)
			assert.Contains(t, logs.String(), "ignoring conflicting managed agents adoption")
			assert.Zero(t, api.updates)
		})
	}
}

func TestAdoptionUsesLastObservedWork(t *testing.T) {
	for _, phase := range []string{"changed identity", "cleared process", "identical duplicate"} {
		t.Run(phase, func(t *testing.T) {
			first := metadataWork(t, "work_test", "proc-first")
			second := metadataWork(t, "work_test", "proc-second")
			second.Data.ID = "session_current"
			second.Metadata[sandboxIDKey] = "00000000-0000-4000-8000-000000000002"
			switch phase {
			case "cleared process":
				delete(second.Metadata, processIDKey)
			case "identical duplicate":
				second = first
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/environments/env_test/work":
					if r.URL.Query().Get("page") == "" {
						writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{first}, "page-two")
					} else {
						writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{second}, "")
					}
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			cfg := testConfig()
			cfg.BaseURL = server.URL
			var logs bytes.Buffer
			launcher, err := New(cfg, &sandboxFixture{
				lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
					instance := readySandbox()
					instance.Id = id
					return instance, nil
				},
				get: func(_ context.Context, _ string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					return &guestpb.Process{ProcessId: req.ProcessId, State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
			}, slog.New(slog.NewTextHandler(&logs, nil)))
			require.NoError(t, err)
			require.NoError(t, launcher.adopt(t.Context()))
			switch phase {
			case "changed identity":
				require.Len(t, launcher.processes, 1)
				entry := launcher.processes["session_current"]
				require.NotNil(t, entry)
				assert.Equal(t, second.Metadata[sandboxIDKey], entry.sandboxID)
				assert.Equal(t, "proc-second", entry.processID)
			case "cleared process":
				requireIdleSandbox(t, launcher, "session_current")
			case "identical duplicate":
				require.Contains(t, launcher.processes, "session_test")
				assert.Equal(t, "proc-first", launcher.processes["session_test"].processID)
			}
			if phase == "identical duplicate" {
				assert.Empty(t, logs.String())
			} else {
				assert.Contains(t, logs.String(), "conflicting managed agents adoption work identities")
				assert.Contains(t, logs.String(), "work_id=work_test")
			}
		})
	}
}

func TestAdoptionKeepsCurrentGenerationAndRetiresExpiredGeneration(t *testing.T) {
	expiredID := "c35730fc-1462-46c9-9298-3f759cad9aff"
	old := metadataWork(t, "work_old", "old-proc")
	old.Metadata[sandboxIDKey] = expiredID
	api := &workAPI{list: func(w http.ResponseWriter, _ *http.Request) {
		writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{old, metadataWork(t, "work_current", "current-proc")}, "")
	}}
	launcher := newTestLauncher(t, &sandboxFixture{lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
		instance := readySandbox()
		instance.Id = id
		instance.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
		if id == expiredID {
			instance.ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
		}
		return instance, nil
	}, get: func(_ context.Context, _ string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
		return &guestpb.Process{ProcessId: req.ProcessId, State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
	}}, api)
	require.NoError(t, launcher.adopt(t.Context()))
	assert.Equal(t, "current-proc", launcher.processes["session_test"].processID)
	assert.NotContains(t, api.metadata["work_old"], processIDKey)
	assert.Equal(t, expiredID, api.metadata["work_old"][sandboxIDKey])
	assert.Equal(t, "preserved", api.metadata["work_old"]["unrelated"])
	assert.Equal(t, "current-proc", api.metadata["work_current"][processIDKey])
}

func TestAdoptionRetiresInactiveRecordsWithoutGuestAccess(t *testing.T) {
	for _, phase := range []string{"suspended", "deleted", "expired", "missing", "suspending"} {
		t.Run(phase, func(t *testing.T) {
			suspends := 0
			api := &workAPI{list: func(w http.ResponseWriter, _ *http.Request) {
				writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{metadataWork(t, "work_test", "proc-a")}, "")
			}}
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(context.Context, string) (*apiv1.Sandbox, error) {
					instance := readySandbox()
					switch phase {
					case "suspended":
						instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
					case "deleted":
						instance.State = apiv1.RuntimeState_RUNTIME_STATE_DELETED
					case "expired":
						instance.ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
					case "missing":
						return nil, serviceerrors.NewNotFound("secret-body", nil)
					case "suspending":
						instance.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND
					}
					return instance, nil
				},
				get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					t.Error("stale process IDs must not cause guest access")
					return nil, errors.New("unexpected guest access")
				},
				suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
					suspends++
					return suspendedSandbox(), nil
				},
			}, api)
			require.NoError(t, launcher.adopt(t.Context()))
			if phase == "suspending" {
				assert.Equal(t, 1, suspends)
			}
			if phase == "missing" {
				assert.Empty(t, launcher.processes)
			} else {
				requireIdleSandbox(t, launcher, "session_test")
			}
			assert.NotContains(t, api.metadata["work_test"], processIDKey)
			assert.Equal(t, testSandboxID, api.metadata["work_test"][sandboxIDKey])
			assert.Equal(t, managedByValue, api.metadata["work_test"][managedByKey])
			assert.Equal(t, "preserved", api.metadata["work_test"]["unrelated"])
			assert.Zero(t, api.beats)
			assert.Zero(t, api.stops)
		})
	}
}

func TestAdoptionSkipsInvalidRecords(t *testing.T) {
	for _, phase := range []string{"bad uuid", "empty process id", "wrong environment", "empty work id", "empty session id", "wrong type"} {
		t.Run(phase, func(t *testing.T) {
			work := metadataWork(t, "work_invalid", "proc-invalid")
			switch phase {
			case "bad uuid":
				work.Metadata[sandboxIDKey] = "secret-body"
			case "empty process id":
				work.Metadata[processIDKey] = " "
			case "wrong environment":
				work.EnvironmentID = "other"
			case "empty work id":
				work.ID = ""
			case "empty session id":
				work.Data.ID = ""
			case "wrong type":
				work.Data.Type = "other"
			}
			api := &workAPI{list: func(w http.ResponseWriter, _ *http.Request) {
				writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{work, metadataWork(t, "work_valid", "proc-valid")}, "")
			}}
			var inspected []string
			launcher := newTestLauncher(t, &sandboxFixture{
				get: func(_ context.Context, _ string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					inspected = append(inspected, req.ProcessId)
					return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
			}, api)
			var logs bytes.Buffer
			launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
			require.NoError(t, launcher.adopt(t.Context()))
			require.Len(t, launcher.processes, 1)
			require.Contains(t, launcher.processes, "session_test")
			assert.Equal(t, "work_valid", launcher.processes["session_test"].workID)
			assert.Equal(t, []string{"proc-valid"}, inspected)
			assert.Contains(t, logs.String(), "ignoring invalid managed agents adoption record")
			assert.NotContains(t, logs.String(), "secret-body")
			assert.Zero(t, api.updates)
		})
	}
}

func TestAdoptionHandlesFailuresAndSanitizesErrors(t *testing.T) {
	for _, phase := range []string{"wrong owner", "wrong template", "not ready", "list failure", "later page failure", "cleanup failure"} {
		t.Run(phase, func(t *testing.T) {
			work := metadataWork(t, "work_test", "proc-a")
			api := &workAPI{list: func(w http.ResponseWriter, r *http.Request) {
				if phase == "list failure" || (phase == "later page failure" && r.URL.Query().Get("page") != "") {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"secret-body standing-environment-secret"}}`)
					return
				}
				next := ""
				if phase == "later page failure" {
					next = "next"
				}
				writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{work}, next)
			}}
			if phase == "cleanup failure" {
				api.update = func(w http.ResponseWriter, _ *http.Request, _ map[string]*string) {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"secret-body"}}`)
				}
			}
			launcher := newTestLauncher(t, &sandboxFixture{lookup: func(context.Context, string) (*apiv1.Sandbox, error) {
				instance := readySandbox()
				switch phase {
				case "wrong owner":
					instance.Creator = "other"
				case "wrong template":
					instance.SandboxTemplate.Name = "other"
				case "not ready":
					instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
					instance.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME
				case "cleanup failure":
					instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
				}
				return instance, nil
			}}, api)
			var logs bytes.Buffer
			launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
			err := launcher.adopt(t.Context())
			if phase == "list failure" || phase == "later page failure" {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "secret-body")
				assert.NotContains(t, err.Error(), testConfig().EnvironmentKey)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, logs.String())
			}
			assert.NotContains(t, logs.String(), "secret-body")
			if phase == "not ready" || phase == "cleanup failure" {
				requireIdleSandbox(t, launcher, "session_test")
			} else {
				assert.Empty(t, launcher.processes)
			}
			assert.Zero(t, api.acks, "failed adoption must not poll")
			assert.Zero(t, api.beats)
			assert.Zero(t, api.stops)
		})
	}
}

func TestRecoverySkipsSandboxLookupFailures(t *testing.T) {
	for _, failure := range []error{
		serviceerrors.NewUnavailable("secret-body", nil),
		serviceerrors.NewPermissionDenied("secret-body", nil),
	} {
		t.Run(string(serviceerrors.CodeOf(failure)), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			work := metadataWork(t, "work_unavailable", "proc-unavailable")
			work.Data.ID = "session_unavailable"
			work.Metadata[sandboxIDKey] = "00000000-0000-4000-8000-000000000002"
			var failedLookups int
			api := &workAPI{
				list: func(w http.ResponseWriter, _ *http.Request) {
					writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{work, metadataWork(t, "work_valid", "proc-valid")}, "")
				},
				onEmpty: cancel,
			}
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
					if id == work.Metadata[sandboxIDKey] {
						failedLookups++
						return nil, failure
					}
					return readySandbox(), nil
				},
				get: func(_ context.Context, _ string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					assert.Equal(t, "proc-valid", req.ProcessId)
					return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
			}, api)
			var logs bytes.Buffer
			launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
			require.NoError(t, launcher.Start(ctx))
			assert.ErrorIs(t, ctx.Err(), context.Canceled, "polling should cancel recovery before the deadline")
			require.Len(t, launcher.processes, 1)
			require.Contains(t, launcher.processes, "session_test")
			assert.Equal(t, "work_valid", launcher.processes["session_test"].workID)
			assert.Equal(t, 1, failedLookups)
			assert.Equal(t, 1, api.lists)
			assert.Zero(t, api.updates)
			assert.Equal(t, "proc-unavailable", api.metadata[work.ID][processIDKey])
			assert.Contains(t, logs.String(), "sandbox lookup failure")
			assert.Contains(t, logs.String(), "work_id=work_unavailable")
			assert.NotContains(t, logs.String(), "secret-body")
		})
	}
}

func TestAdoptionIgnoresUnrelatedAndEmptyMetadata(t *testing.T) {
	unrelated, cleared := testWork("unrelated"), testWork("cleared")
	unrelated.Metadata = map[string]string{"someone-else": "not-json"}
	cleared.Metadata = map[string]string{managedByKey: managedByValue, sandboxIDKey: testSandboxID}
	foreign := metadataWork(t, "foreign", "foreign-proc")
	foreign.Metadata[managedByKey] = "other"
	api := &workAPI{list: func(w http.ResponseWriter, _ *http.Request) {
		writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{unrelated, cleared, foreign}, "")
	}}
	launcher := newTestLauncher(t, &sandboxFixture{lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
		assert.Equal(t, testSandboxID, id)
		return readySandbox(), nil
	}}, api)
	require.NoError(t, launcher.adopt(t.Context()))
	requireIdleSandbox(t, launcher, "session_test")
	assert.Zero(t, api.updates)
}

func TestRepeatedLiveWorkNeverRelaunches(t *testing.T) {
	var starts atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	api := &workAPI{update: func(w http.ResponseWriter, _ *http.Request, values map[string]*string) {
		if values[processIDKey] != nil && fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}}
	launcher := newTestLauncher(t, &sandboxFixture{
		start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			starts.Add(1)
			return &guestpb.Process{ProcessId: "only-process", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
		get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
			return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
	}, api)
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	require.ErrorContains(t, launcher.launch(ctx, testWork("work_test")), "CLI is running")
	assert.NotContains(t, api.metadata["work_test"], processIDKey)
	fail.Store(false)
	var repeats sync.WaitGroup
	for range 5 {
		repeats.Go(func() { assert.NoError(t, launcher.launch(t.Context(), testWork("work_test"))) })
	}
	repeats.Wait()
	assert.EqualValues(t, 1, starts.Load())
	assert.Equal(t, 1, api.beats)
	assert.Equal(t, "only-process", api.metadata["work_test"][processIDKey])
	assert.Equal(t, "only-process", launcher.processes["session_test"].processID)
}

func TestRecoveryHandlesFailuresBeforePolling(t *testing.T) {
	for _, phase := range []string{"list", "inspect", "suspend", "metadata"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var failures, successes atomic.Int32
			api := &workAPI{
				list: func(w http.ResponseWriter, _ *http.Request) {
					if phase == "list" && failures.CompareAndSwap(0, 1) {
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"secret-body"}}`)
						return
					}

					healthy := metadataWork(t, "work_healthy", "proc-healthy")
					healthy.Data.ID = "session_healthy"
					healthy.Metadata[sandboxIDKey] = "00000000-0000-4000-8000-000000000002"
					writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{metadataWork(t, "work_test", "proc"), healthy}, "")
				},
				onEmpty: func() {
					assert.EqualValues(t, 1, failures.Load())
					if phase == "list" {
						assert.Positive(t, successes.Load(), "list recovery must finish before any poll")
					}
					cancel()
				},
			}
			api.update = func(w http.ResponseWriter, _ *http.Request, values map[string]*string) {
				assert.Contains(t, values, processIDKey)
				assert.Nil(t, values[processIDKey], "cleanup must encode actual JSON null")
				assert.Len(t, values, 1, "cleanup deletes only the process key")
				if phase == "metadata" && failures.CompareAndSwap(0, 1) {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				successes.Add(1)
				_, _ = io.WriteString(w, `{}`)
			}
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
					sb := readySandbox()
					if id != testSandboxID {
						sb.Id = id
						return sb, nil
					}
					if phase == "suspend" {
						sb.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND
					}
					if phase == "metadata" {
						sb.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
					}
					return sb, nil
				},
				get: func(_ context.Context, _ string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					if req.ProcessId == "proc-healthy" {
						return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
					}
					if phase == "inspect" && failures.CompareAndSwap(0, 1) {
						return nil, serviceerrors.NewUnavailable("secret-body", nil)
					}
					return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
				},
				suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
					if phase == "suspend" && failures.CompareAndSwap(0, 1) {
						return nil, serviceerrors.NewUnavailable("secret-body", nil)
					}
					return suspendedSandbox(), nil
				},
				create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
					t.Error("recovery must never create or launch")
					return nil, errors.New("unexpected create")
				},
			}, api)
			require.NoError(t, launcher.Start(ctx))
			if phase == "inspect" || phase == "suspend" {
				require.Contains(t, launcher.processes, "session_test")
				assert.Equal(t, "proc", launcher.processes["session_test"].processID)
			} else {
				requireIdleSandbox(t, launcher, "session_test")
			}
			require.Contains(t, launcher.processes, "session_healthy")
			assert.Equal(t, "proc-healthy", launcher.processes["session_healthy"].processID)
			if phase != "list" {
				assert.Equal(t, 1, api.lists, "per-record failures must not restart adoption")
			}
			assert.Zero(t, api.beats)
			assert.Zero(t, api.stops)
			assert.Equal(t, "preserved", api.metadata["work_test"]["unrelated"])
			assert.Equal(t, managedByValue, api.metadata["work_test"][managedByKey])
			assert.Equal(t, testSandboxID, api.metadata["work_test"][sandboxIDKey])
			if phase == "list" {
				assert.NotContains(t, api.metadata["work_test"], processIDKey)
			} else {
				assert.Equal(t, "proc", api.metadata["work_test"][processIDKey])
			}
		})
	}
}

func TestSuspensionFailureRetainsWorkIdentity(t *testing.T) {
	api := &workAPI{metadata: map[string]map[string]string{
		"first": {"customer": "first-value"},
	}}
	var suspends int
	launcher := newTestLauncher(t, &sandboxFixture{
		start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			return &guestpb.Process{ProcessId: "proc-" + req.Env["ANTHROPIC_WORK_ID"], State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
		suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
			suspends++
			if suspends == 1 {
				return nil, serviceerrors.NewUnavailable("secret-body", nil)
			}
			return suspendedSandbox(), nil
		},
	}, api)
	require.NoError(t, launcher.launch(t.Context(), testWork("first")))
	launcher.collect(t.Context())
	assert.Equal(t, "proc-first", api.metadata["first"][processIDKey])
	launcher.collect(t.Context())
	requireIdleSandbox(t, launcher, "session_test")
	assert.NotContains(t, api.metadata["first"], processIDKey)
	assert.Equal(t, testSandboxID, api.metadata["first"][sandboxIDKey])
	assert.Equal(t, managedByValue, api.metadata["first"][managedByKey])
	assert.Equal(t, "first-value", api.metadata["first"]["customer"])
}

func TestRecoveryPermanentFailureDoesNotPoll(t *testing.T) {
	api := &workAPI{list: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"permission_error","message":"secret-body"}}`)
	}}
	launcher := newTestLauncher(t, &sandboxFixture{}, api)
	require.Error(t, launcher.Start(t.Context()))
	assert.Equal(t, 1, api.lists)
	assert.Zero(t, api.acks)
	assert.Zero(t, api.beats)
}

func TestMetadataTransientWriteRetriesWithoutRelaunch(t *testing.T) {
	var publications, starts int
	api := &workAPI{update: func(w http.ResponseWriter, _ *http.Request, values map[string]*string) {
		if values[processIDKey] != nil {
			publications++
			if publications == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		_, _ = io.WriteString(w, `{}`)
	}}
	launcher := newTestLauncher(t, &sandboxFixture{start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
		starts++
		return &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
	}}, api)
	require.NoError(t, launcher.launch(t.Context(), testWork("work")))
	assert.Equal(t, 1, starts)
	assert.Equal(t, 2, publications)
	assert.Equal(t, "worker", api.metadata["work"][processIDKey])
}

func TestRepeatedAssignmentDuringProvisioningUsesOneWorker(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var starts atomic.Int32
	api := &workAPI{}
	launcher := newTestLauncher(t, &sandboxFixture{
		create: func(ctx context.Context, _ *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			close(entered)
			select {
			case <-release:
				return readySandbox(), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			starts.Add(1)
			return &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
		get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
			return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
	}, api)
	done := make(chan error, 2)
	go func() { done <- launcher.launch(t.Context(), testWork("work")) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first assignment did not provision")
	}
	go func() { done <- launcher.launch(t.Context(), testWork("work")) }()
	close(release)
	for range 2 {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("repeat assignment did not finish")
		}
	}
	assert.EqualValues(t, 1, starts.Load())
	assert.Equal(t, 1, api.beats)
	assert.Equal(t, "worker", launcher.processes["session_test"].processID)
}
