package a2a

// RuntimeIdentityPath is projected by Substrate and rebound on restore. It is
// routing metadata. Temporary TaskStore headers use it until Substrate #1660.
const RuntimeIdentityPath = "/run/kagent/identity/name"

// InsecureRuntimeIdentityHeader temporarily carries atespace/actor-name/actor-UID
// until Substrate supplies verified actor credentials (#1660).
const InsecureRuntimeIdentityHeader = "x-kagent-insecure-runtime-identity"

// DispatchHeader carries the gateway's attempt fence to the runtime TaskStore.
const DispatchHeader = "x-kagent-dispatch-id"

// WorkingDirectoryHeader carries the directory a turn works in: the session's
// own directory of its volume source. A runtime reads it per turn, never at
// boot, since a runtime restored from a snapshot predates the session's
// mounts; a turn without it works in the runtime's default directory.
const WorkingDirectoryHeader = "x-kagent-working-directory"

// MirrorsDirectoryHeader carries the read-only directory of the session's
// volume that holds bare git mirrors, as <owner>/<repository>.git, which a
// runtime clones into the working directory before the session's first turn.
// A turn without it has no mirrors to clone.
const MirrorsDirectoryHeader = "x-kagent-mirrors-directory"

// ChangedRepositoriesHeader carries the repositories, <owner>/<repository>
// relative to the working directory, whose origin moved since the mirrors'
// last sync, comma-separated: the ones the session's admission named. A
// runtime fetches exactly these before the session's first turn; an absent
// header fetches nothing.
const ChangedRepositoriesHeader = "x-kagent-changed-repositories"
