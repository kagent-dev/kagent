package managedagents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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

func provisionTestSandbox(t *testing.T, ctx context.Context, launcher *Launcher) (*apiv1.Sandbox, error) {
	t.Helper()
	work := testWork("work_test")
	entry, err := launcher.lockSandbox(ctx, work.Data.ID)
	if err != nil {
		return nil, err
	}
	defer launcher.unlockSandbox(work.Data.ID, entry)
	return launcher.provision(ctx, work, entry)
}

func savedWork(workID, sandboxID string) *anthropic.BetaSelfHostedWork {
	work := testWork(workID)
	work.Secret = ""
	work.Metadata = map[string]string{managedByKey: managedByValue, sandboxIDKey: sandboxID}
	return work
}

func TestIdleSandboxReferenceSurvivesCollectionAndRestart(t *testing.T) {
	instance := readySandbox()
	instance.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
	creates, resumes, starts := 0, 0, 0
	api := &workAPI{}
	fixture := &sandboxFixture{
		lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
			assert.Equal(t, instance.Id, id)
			return instance, nil
		},
		create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			creates++
			return instance, nil
		},
		suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
			instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
			return instance, nil
		},
		resume: func(context.Context, string) (*apiv1.Sandbox, error) {
			resumes++
			instance.State = apiv1.RuntimeState_RUNTIME_STATE_READY
			return instance, nil
		},
		start: func(_ context.Context, id string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			starts++
			assert.Equal(t, id, api.metadata[req.Env["ANTHROPIC_WORK_ID"]][sandboxIDKey])
			assert.NotContains(t, api.metadata[req.Env["ANTHROPIC_WORK_ID"]], processIDKey)
			return &guestpb.Process{ProcessId: req.Env["ANTHROPIC_WORK_ID"], State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
	}
	first := newTestLauncher(t, fixture, api)
	require.NoError(t, first.launch(t.Context(), testWork("first")))
	first.collect(t.Context())
	requireIdleSandbox(t, first, "session_test")
	assert.NotContains(t, api.metadata["first"], processIDKey)
	require.Equal(t, instance.Id, api.metadata["first"][sandboxIDKey])

	second := newTestLauncher(t, fixture, api)
	require.NoError(t, second.adopt(t.Context()))
	requireIdleSandbox(t, second, "session_test")
	require.NoError(t, second.launch(t.Context(), testWork("second")))
	assert.Equal(t, 1, creates)
	assert.Equal(t, 1, resumes)
	assert.Equal(t, 2, starts)
	assert.Equal(t, instance.Id, api.metadata["first"][sandboxIDKey])
	assert.Equal(t, instance.Id, api.metadata["second"][sandboxIDKey])
	assert.Equal(t, 1, api.lists, "only startup scans Work metadata")
	second.collect(t.Context())
	requireIdleSandbox(t, second, "session_test")
	require.NoError(t, second.launch(t.Context(), testWork("third")))
	assert.Equal(t, 1, creates, "idle reuse keeps the original sandbox")
	assert.Equal(t, 2, resumes)
	assert.Equal(t, 3, starts)
	assert.Equal(t, 1, api.lists, "subsequent launches use the session index")
}

func TestSuspendedSandboxStaleMetadataMustClearBeforeResume(t *testing.T) {
	instance := readySandbox()
	instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
	work := metadataWork(t, "previous", "old-process")
	fail := true
	resumes := 0
	api := &workAPI{list: func(w http.ResponseWriter, _ *http.Request) {
		writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{work}, "")
	}}
	api.update = func(w http.ResponseWriter, _ *http.Request, values map[string]*string) {
		if fail && values[processIDKey] == nil && values[sandboxIDKey] == nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}
	launcher := newTestLauncher(t, &sandboxFixture{
		lookup: func(context.Context, string) (*apiv1.Sandbox, error) { return instance, nil },
		create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			t.Error("a suspended sandbox must not be replaced")
			return nil, errors.New("unexpected create")
		},
		get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
			t.Error("a suspended guest must not be inspected")
			return nil, errors.New("unexpected guest access")
		},
		resume: func(context.Context, string) (*apiv1.Sandbox, error) {
			resumes++
			assert.NotContains(t, api.metadata["previous"], processIDKey, "cleanup must precede resume")
			instance.State = apiv1.RuntimeState_RUNTIME_STATE_READY
			return instance, nil
		},
	}, api)
	require.NoError(t, launcher.adopt(t.Context()))
	_, err := provisionTestSandbox(t, t.Context(), launcher)
	require.Error(t, err)
	assert.Zero(t, resumes)
	assert.Equal(t, "old-process", api.metadata["previous"][processIDKey])
	assert.NotContains(t, api.metadata["work_test"], sandboxIDKey)

	fail = false
	_, err = provisionTestSandbox(t, t.Context(), launcher)
	require.NoError(t, err)
	assert.Equal(t, 1, resumes)
	assert.NotContains(t, api.metadata["previous"], processIDKey)
	assert.Equal(t, testSandboxID, api.metadata["previous"][sandboxIDKey])
	assert.Equal(t, "preserved", api.metadata["previous"]["unrelated"])
	assert.Equal(t, 1, api.lists)
}

func TestSavedGenerationSelectionIgnoresPageOrder(t *testing.T) {
	for _, latestFirst := range []bool{false, true} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("latest_first=%t/expired=%t", latestFirst, expired), func(t *testing.T) {
				now := time.Now()
				old, latest := readySandbox(), readySandbox()
				old.CreatedAt = timestamppb.New(now.Add(-4 * time.Hour))
				old.ExpiresAt = timestamppb.New(now.Add(-3 * time.Hour))
				old.State = apiv1.RuntimeState_RUNTIME_STATE_DELETED
				latest.Id = "00000000-0000-4000-8000-000000000002"
				latest.CreatedAt = timestamppb.New(now.Add(-30 * time.Minute))
				latest.ExpiresAt = timestamppb.New(now.Add(30 * time.Minute))
				latest.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
				if expired {
					latest.CreatedAt = nil // Expiry also orders the fixed-lifetime generations.
					latest.ExpiresAt = timestamppb.New(now.Add(-time.Minute))
					latest.State = apiv1.RuntimeState_RUNTIME_STATE_DELETED
				}
				works := []*anthropic.BetaSelfHostedWork{savedWork("old", old.Id), savedWork("latest", latest.Id)}
				if latestFirst {
					works[0], works[1] = works[1], works[0]
				}
				api := &workAPI{list: func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Query().Get("page") == "" {
						writeWorkPage(t, w, works[:1], "next")
					} else {
						writeWorkPage(t, w, works[1:], "")
					}
				}}
				creates, resumes := 0, 0
				successor := readySandbox()
				successor.Id = "00000000-0000-4000-8000-000000000003"
				launcher := newTestLauncher(t, &sandboxFixture{
					lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
						if id == old.Id {
							return old, nil
						}
						if id == successor.Id {
							return successor, nil
						}
						return latest, nil
					},
					create: func(_ context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
						creates++
						assert.True(t, expired, "live saved generations must not call Create")
						assert.Equal(t, replacementRequest(testConfig(), "session_test", latest.Id).RequestId, req.RequestId)
						return successor, nil
					},
					resume: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
						resumes++
						assert.Equal(t, latest.Id, id)
						latest.State = apiv1.RuntimeState_RUNTIME_STATE_READY
						return latest, nil
					},
				}, api)
				require.NoError(t, launcher.adopt(t.Context()))
				instance, err := provisionTestSandbox(t, t.Context(), launcher)
				require.NoError(t, err)
				if expired {
					assert.Equal(t, successor.Id, instance.Id)
					assert.Equal(t, 1, creates)
					assert.Zero(t, resumes)
				} else {
					assert.Equal(t, latest.Id, instance.Id)
					assert.Zero(t, creates)
					assert.Equal(t, 1, resumes)
				}
				assert.Equal(t, 2, api.lists)
				assert.Equal(t, instance.Id, api.metadata["work_test"][sandboxIDKey])
				assert.NotContains(t, api.metadata["work_test"], processIDKey)
				reused, err := provisionTestSandbox(t, t.Context(), launcher)
				require.NoError(t, err)
				assert.Equal(t, instance.Id, reused.Id, "the selected or replacement sandbox stays indexed")
				assert.Equal(t, 2, api.lists, "reuse must not repeat startup discovery")
				assert.LessOrEqual(t, creates, 1)
			})
		}
	}
}

func TestIndexedSandboxLookupFailsClosed(t *testing.T) {
	for _, phase := range []string{"missing", "manual deletion", "wrong owner", "wrong template", "wrong identity", "lookup forbidden"} {
		t.Run(phase, func(t *testing.T) {
			instance := readySandbox()
			instance.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
			api := &workAPI{}
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
					switch phase {
					case "missing":
						return nil, serviceerrors.NewNotFound("secret-body", nil)
					case "lookup forbidden":
						return nil, serviceerrors.NewPermissionDenied("secret-body", nil)
					case "manual deletion":
						instance.State = apiv1.RuntimeState_RUNTIME_STATE_DELETED
					case "wrong owner":
						instance.Creator = "other"
					case "wrong template":
						instance.SandboxTemplate.Name = "other"
					}
					if phase != "wrong identity" {
						instance.Id = id
					} else {
						instance.Id = ""
					}
					return instance, nil
				},
				create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
					t.Error("discovery failure must not create or replace sandboxes")
					return nil, errors.New("unexpected create")
				},
			}, api)
			var logs bytes.Buffer
			launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
			launcher.processes["session_test"] = &sandboxWorker{sandboxID: testSandboxID}
			instance, err := provisionTestSandbox(t, t.Context(), launcher)
			require.Error(t, err)
			assert.Nil(t, instance)
			assert.NotContains(t, err.Error(), "secret-body")
			assert.NotContains(t, logs.String(), "secret-body")
			assert.NotContains(t, api.metadata["work_test"], sandboxIDKey)
			assert.Zero(t, api.lists)
		})
	}
}

func TestPrelaunchReferenceRetriesBeforeStartingWorker(t *testing.T) {
	for _, phase := range []string{"lookup", "publication"} {
		t.Run(phase, func(t *testing.T) {
			failures, starts, publications := 0, 0, 0
			initialized := false
			instance := readySandbox()
			api := &workAPI{list: func(w http.ResponseWriter, _ *http.Request) {
				writeWorkPage(t, w, []*anthropic.BetaSelfHostedWork{savedWork("previous", instance.Id)}, "")
			}}
			api.update = func(w http.ResponseWriter, _ *http.Request, values map[string]*string) {
				if values[sandboxIDKey] != nil && values[processIDKey] == nil {
					publications++
					if phase == "publication" && failures == 0 {
						failures++
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
				}
				_, _ = io.WriteString(w, `{}`)
			}
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(context.Context, string) (*apiv1.Sandbox, error) {
					if initialized && phase == "lookup" && failures == 0 {
						failures++
						return nil, serviceerrors.NewUnavailable("secret-body", nil)
					}
					return instance, nil
				},
				create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
					t.Error("saved live reference must not be recreated")
					return nil, errors.New("unexpected create")
				},
				start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					starts++
					assert.Equal(t, instance.Id, api.metadata["work_test"][sandboxIDKey])
					assert.NotContains(t, api.metadata["work_test"], processIDKey)
					assert.Equal(t, 1, failures)
					return &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
			}, api)
			require.NoError(t, launcher.adopt(t.Context()))
			initialized = true
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			require.NoError(t, launcher.launch(ctx, testWork("work_test")))
			assert.Equal(t, 1, starts)
			assert.Equal(t, 1, api.lists)
			if phase == "publication" {
				assert.Equal(t, 2, publications)
			}
		})
	}
}

func TestFailedProvisionOrStartRetainsPrelaunchReference(t *testing.T) {
	for _, phase := range []string{"lifecycle", "resume", "start"} {
		t.Run(phase, func(t *testing.T) {
			instance := readySandbox()
			if phase == "lifecycle" {
				instance.State = apiv1.RuntimeState_RUNTIME_STATE_FAILED
			}
			if phase == "resume" {
				instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
			}
			api := &workAPI{metadata: map[string]map[string]string{"work_test": {"customer": "retained"}}}
			launcher := newTestLauncher(t, &sandboxFixture{
				create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) { return instance, nil },
				resume: func(context.Context, string) (*apiv1.Sandbox, error) {
					return nil, serviceerrors.NewPermissionDenied("denied", nil)
				},
				start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					assert.Equal(t, instance.Id, api.metadata["work_test"][sandboxIDKey])
					assert.NotContains(t, api.metadata["work_test"], processIDKey)
					return nil, errors.New("definitive start failure")
				},
			}, api)
			require.Error(t, launcher.launch(t.Context(), testWork("work_test")))
			assert.Equal(t, instance.Id, api.metadata["work_test"][sandboxIDKey])
			assert.Equal(t, managedByValue, api.metadata["work_test"][managedByKey])
			assert.Equal(t, "retained", api.metadata["work_test"]["customer"])
			assert.NotContains(t, api.metadata["work_test"], processIDKey)
			assert.Equal(t, 1, api.stops)
		})
	}
}

func TestExpiredUnpublishedReceiptFailsClosed(t *testing.T) {
	expiredReceipt := serviceerrors.NewFailedPrecondition("expired receipt", nil)
	var requests []string
	api := &workAPI{}
	fixture := &sandboxFixture{create: func(_ context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
		requests = append(requests, req.RequestId)
		if len(requests) == 1 {
			return nil, serviceerrors.NewUnavailable("creation response lost; no ID was published", nil)
		}
		return nil, expiredReceipt
	}}
	for range 2 {
		instance, err := provisionTestSandbox(t, t.Context(), newTestLauncher(t, fixture, api))
		require.ErrorIs(t, err, expiredReceipt)
		require.ErrorContains(t, err, "without saved successor expiry evidence")
		assert.Nil(t, instance)
	}
	require.Len(t, requests, 3)
	for _, id := range requests {
		assert.Equal(t, createRequest(testConfig(), "session_test").RequestId, id, "never invent a replacement without an expired saved sandbox")
	}
	assert.NotContains(t, api.metadata["work_test"], sandboxIDKey)
}

func TestFailedPrelaunchPublicationCannotStartAndLiveReceiptCanRecover(t *testing.T) {
	denied := true
	attempts, starts := 0, 0
	var receiptID string
	instance := readySandbox()
	api := &workAPI{update: func(w http.ResponseWriter, _ *http.Request, values map[string]*string) {
		if values[sandboxIDKey] != nil && values[processIDKey] == nil && denied {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"permission_error","message":"secret-body"}}`)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}}
	fixture := &sandboxFixture{
		create: func(_ context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			attempts++
			if receiptID == "" {
				receiptID = req.RequestId
			}
			assert.Equal(t, receiptID, req.RequestId, "a live unpublished creation reuses the deterministic receipt")
			return instance, nil
		},
		start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			starts++
			assert.Equal(t, instance.Id, api.metadata[req.Env["ANTHROPIC_WORK_ID"]][sandboxIDKey])
			return &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
	}
	first := newTestLauncher(t, fixture, api)
	err := first.launch(t.Context(), testWork("first"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-body")
	assert.NotContains(t, api.metadata["first"], sandboxIDKey, "failed patches must not apply")
	assert.Zero(t, starts)
	denied = false
	second := newTestLauncher(t, fixture, api)
	require.NoError(t, second.launch(t.Context(), testWork("second")))
	assert.Equal(t, 2, attempts)
	assert.Equal(t, 1, starts)
	assert.Equal(t, instance.Id, api.metadata["second"][sandboxIDKey])
}
