package anthropicselfhostedsandboxes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

func TestSandboxGenerationRequiresCreationTime(t *testing.T) {
	instance := readySandbox()
	instance.CreatedAt = nil
	instance.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
	_, err := sandboxGeneration(instance)
	require.ErrorContains(t, err, "generation time is missing")
}

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

func TestIndexedSandboxLookupFailsClosed(t *testing.T) {
	for _, phase := range []string{"missing", "manual deletion", "wrong owner", "wrong template", "wrong identity", "lookup forbidden"} {
		t.Run(phase, func(t *testing.T) {
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
					instance := readySandbox()
					switch phase {
					case "missing":
						return nil, serviceerrors.NewNotFound("secret-body", nil)
					case "lookup forbidden":
						return nil, serviceerrors.NewPermissionDenied("secret-body", nil)
					case "manual deletion":
						instance.State = apiv1.RuntimeState_RUNTIME_STATE_DELETED
						instance.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
					case "wrong owner":
						instance.Creator = "other"
					case "wrong template":
						instance.SandboxTemplate.Name = "other"
					case "wrong identity":
						instance.Id = ""
					}
					return instance, nil
				},
				create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
					t.Error("discovery failure must not create or replace sandboxes")
					return nil, errors.New("unexpected create")
				},
				resume: func(context.Context, string) (*apiv1.Sandbox, error) {
					t.Error("invalid saved sandbox must not be resumed")
					return nil, errors.New("unexpected resume")
				},
			}, &workAPI{})
			entry := &sandboxWorker{sandboxID: testSandboxID}
			launcher.processes["session_test"] = entry
			instance, err := provisionTestSandbox(t, t.Context(), launcher)
			require.Error(t, err)
			assert.Nil(t, instance)
			assert.NotContains(t, err.Error(), "secret-body")
			assert.Same(t, entry, launcher.processes["session_test"])
			assert.Equal(t, testSandboxID, entry.sandboxID)
		})
	}
}
