package a2a

import (
	"path"
	"strings"
)

// RuntimeIdentityPath is projected by Substrate and rebound on restore. It is
// routing metadata, not an authentication credential.
const RuntimeIdentityPath = "/run/kagent/identity/name"

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
// runtime fetches exactly these before the session's first turn, and again on
// a later turn those whose fetch failed; an absent header fetches nothing. An
// entry that is not <owner>/<repository> (ValidRepository) is reported, never
// fetched.
const ChangedRepositoriesHeader = "x-kagent-changed-repositories"

// ValidRepository reports whether repository is <owner>/<repository>, the
// only form a ChangedRepositoriesHeader entry takes: one clean path segment
// each, neither . nor .., so it names a clone directly under the working
// directory. Whoever admits a session's changed repositories refuses what a
// runtime would not fetch.
func ValidRepository(repository string) bool {
	owner, name, ok := strings.Cut(repository, "/")
	return ok && owner != "" && name != "" && !strings.Contains(name, "/") && path.Clean(repository) == repository &&
		owner != ".." && name != ".." && owner != "." && name != "."
}
