package substrate

import (
	"strings"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func testVolumeSource() *apiv1alpha1.SessionVolumeSource {
	return &apiv1alpha1.SessionVolumeSource{
		Volume: &apiv1alpha1.SessionVolume{CsiDriver: "nfs.csi.k8s.io", VolumeHandle: "nfs-server#share#workspace-a##"},
		Mounts: []*apiv1alpha1.SessionVolumeMount{
			{SubPath: "sessions/${SESSION_ID}", MountPath: WorkspaceMountPath},
			{SubPath: "mirrors", MountPath: MirrorsMountPath, ReadOnly: true},
		},
	}
}

func TestSessionExistingVolumes(t *testing.T) {
	volumes, err := SessionExistingVolumes("0199a6f0-session", testVolumeSource())
	require.NoError(t, err)
	want := []*ateapipb.ExistingVolume{
		{Name: workspaceVolume, Driver: "nfs.csi.k8s.io", VolumeHandle: "nfs-server#share#workspace-a##",
			AccessMode: ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY, SubPath: "sessions/0199a6f0-session"},
		{Name: mirrorsVolume, Driver: "nfs.csi.k8s.io", VolumeHandle: "nfs-server#share#workspace-a##",
			AccessMode: ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_ONLY_MANY, SubPath: "mirrors"},
	}
	require.Len(t, volumes, len(want))
	for i := range want {
		require.True(t, proto.Equal(want[i], volumes[i]), "volume %d: %v", i, volumes[i])
	}

	none, err := SessionExistingVolumes("0199a6f0-session", nil)
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestValidateWorkspaceLayout(t *testing.T) {
	for name, test := range map[string]struct {
		mutate  func(*apiv1alpha1.SessionVolumeSource)
		wantErr string
	}{
		"the layout":         {mutate: func(*apiv1alpha1.SessionVolumeSource) {}},
		"own directory only": {mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts = s.Mounts[:1] }},
		"no own directory": {
			mutate:  func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts = s.Mounts[1:] },
			wantErr: "read-write at /workspace",
		},
		"own directory read-only": {
			mutate:  func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].ReadOnly = true },
			wantErr: "not one of the workspace layout",
		},
		"mirrors read-write": {
			mutate:  func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[1].ReadOnly = false },
			wantErr: "not one of the workspace layout",
		},
		"another path": {
			mutate:  func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[1].MountPath = "/srv/mirrors" },
			wantErr: "not one of the workspace layout",
		},
		"handle longer than Substrate takes": {
			mutate:  func(s *apiv1alpha1.SessionVolumeSource) { s.Volume.VolumeHandle = strings.Repeat("h", 257) },
			wantErr: "at most 256",
		},
	} {
		t.Run(name, func(t *testing.T) {
			source := testVolumeSource()
			test.mutate(source)
			err := ValidateWorkspaceLayout(source)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestWorkingDirectory(t *testing.T) {
	require.Equal(t, "/workspace", WorkingDirectory(&apiv1alpha1.Session{VolumeSource: testVolumeSource()}))
	require.Equal(t, "/data/workspace", WorkingDirectory(&apiv1alpha1.Session{}))
}

// TestActorTemplateDeclaresTheWorkspaceVolumes proves every revision's
// template declares the two existing volumes at the fixed layout, which an
// actor without a volume source leaves unsupplied.
func TestActorTemplateDeclaresTheWorkspaceVolumes(t *testing.T) {
	spec := &translator.Revision{
		Namespace: "agents", AgentName: "helper", Image: "agent.example/image:v1",
		WorkerPoolName: "default", SnapshotLocation: "snapshots", ConfigJSON: []byte(`{}`),
		AgentCard: &a2apb.AgentCard{Name: "helper", Version: "v1", Capabilities: &a2apb.AgentCapabilities{},
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://127.0.0.1:80", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"}},
	}
	revisionID, err := spec.Digest()
	require.NoError(t, err)
	template, err := ActorTemplateForRevision(spec, revisionID)
	require.NoError(t, err)
	declared := map[string]bool{}
	for _, volume := range template.GetVolumes() {
		if volume.GetExistingVolume() != nil {
			declared[volume.GetName()] = true
		}
	}
	require.Equal(t, map[string]bool{workspaceVolume: true, mirrorsVolume: true}, declared)
	mounts := map[string]*ateapipb.VolumeMount{}
	for _, mount := range template.GetContainers()[0].GetVolumeMounts() {
		mounts[mount.GetName()] = mount
	}
	require.Equal(t, WorkspaceMountPath, mounts[workspaceVolume].GetMountPath())
	require.False(t, mounts[workspaceVolume].GetReadOnly())
	require.Equal(t, MirrorsMountPath, mounts[mirrorsVolume].GetMountPath())
	require.True(t, mounts[mirrorsVolume].GetReadOnly())
}
