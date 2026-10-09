package substrate

import (
	"fmt"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// A Session's workspace volume is mounted at a fixed layout: Substrate fixes
// an existing volume's mount paths in the ActorTemplate, which every Session of
// a revision shares, so every revision's template declares the same two
// existing volumes and each Session's actor supplies the directories of its
// own volume source for them. An actor that supplies none gets neither mount.
const (
	// WorkspaceMountPath is where a Session sees its own directory of the
	// volume, read-write: the working directory of its turns.
	WorkspaceMountPath = "/workspace"
	// MirrorsMountPath is where a Session sees the volume's shared directory,
	// read-only, such as the git mirrors its clones borrow objects from.
	MirrorsMountPath = "/mirrors"
	// DurableWorkingDirectory is the working directory of a Session without a
	// volume: a directory of its durable /data.
	DurableWorkingDirectory = durableDataMount + "/workspace"

	workspaceVolume = "workspace"
	mirrorsVolume   = "workspace-mirrors"
	// SessionIDPlaceholder stands for the Session's id in a mount's sub-path.
	SessionIDPlaceholder = "${SESSION_ID}"
	// maxVolumeHandleLength is ExistingVolume.volume_handle's maxLength.
	maxVolumeHandleLength = 256
)

// workspaceVolumes are the existing volumes every revision's ActorTemplate
// declares, and workspaceVolumeMounts their mounts in its container.
func workspaceVolumes() []*ateapipb.Volume {
	return []*ateapipb.Volume{
		{Name: workspaceVolume, ExistingVolume: &ateapipb.ExistingVolumeSource{}},
		{Name: mirrorsVolume, ExistingVolume: &ateapipb.ExistingVolumeSource{}},
	}
}

func workspaceVolumeMounts() []*ateapipb.VolumeMount {
	return []*ateapipb.VolumeMount{
		{Name: workspaceVolume, MountPath: WorkspaceMountPath},
		{Name: mirrorsVolume, MountPath: MirrorsMountPath, ReadOnly: true},
	}
}

// WorkingDirectory is the directory a Session's turns work in: its own
// directory of the workspace volume, or a directory of its durable /data.
func WorkingDirectory(session *apiv1alpha1.Session) string {
	if session.GetVolumeSource() != nil {
		return WorkspaceMountPath
	}
	return DurableWorkingDirectory
}

// ValidateWorkspaceLayout refuses a volume source whose mounts the
// ActorTemplate does not declare: its own directory read-write at
// WorkspaceMountPath, and optionally a read-only directory at
// MirrorsMountPath.
func ValidateWorkspaceLayout(source *apiv1alpha1.SessionVolumeSource) error {
	if handle := source.GetVolume().GetVolumeHandle(); len(handle) > maxVolumeHandleLength {
		return fmt.Errorf("volume_handle is %d characters; Substrate supports at most %d", len(handle), maxVolumeHandleLength)
	}
	workspace := false
	for _, mount := range source.GetMounts() {
		switch {
		case mount.GetMountPath() == WorkspaceMountPath && !mount.GetReadOnly():
			workspace = true
		case mount.GetMountPath() == MirrorsMountPath && mount.GetReadOnly():
		default:
			return fmt.Errorf("mount at %s (read_only %t) is not one of the workspace layout: the Session's own directory read-write at %s and the shared directory read-only at %s",
				mount.GetMountPath(), mount.GetReadOnly(), WorkspaceMountPath, MirrorsMountPath)
		}
	}
	if !workspace {
		return fmt.Errorf("a volume source mounts the Session's own directory read-write at %s", WorkspaceMountPath)
	}
	return nil
}

// SessionExistingVolumes is what a Session's actor supplies for the
// template's workspace volumes: each mount of its volume source, the
// placeholder in its sub-path replaced by the Session's id. A Session without
// a volume source supplies none.
func SessionExistingVolumes(sessionID string, source *apiv1alpha1.SessionVolumeSource) ([]*ateapipb.ExistingVolume, error) {
	if source == nil {
		return nil, nil
	}
	if err := ValidateWorkspaceLayout(source); err != nil {
		return nil, err
	}
	volumes := make([]*ateapipb.ExistingVolume, 0, len(source.GetMounts()))
	for _, mount := range source.GetMounts() {
		volume := &ateapipb.ExistingVolume{
			Name:         workspaceVolume,
			Driver:       source.GetVolume().GetCsiDriver(),
			VolumeHandle: source.GetVolume().GetVolumeHandle(),
			AccessMode:   ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY,
			SubPath:      strings.ReplaceAll(mount.GetSubPath(), SessionIDPlaceholder, sessionID),
		}
		if mount.GetMountPath() == MirrorsMountPath {
			volume.Name, volume.AccessMode = mirrorsVolume, ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_ONLY_MANY
		}
		volumes = append(volumes, volume)
	}
	return volumes, nil
}
