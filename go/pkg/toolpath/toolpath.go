// Package toolpath is the contract between the controller and every runtime
// for Harness tool images: where the controller mounts them and how a runtime
// exposes their binaries on PATH.
package toolpath

import (
	"slices"
	"strings"
)

const (
	// EnvName carries the colon-joined bin directories of the mounted tool images.
	EnvName = "KAGENT_TOOL_PATHS"
	// MountRoot is the directory under which each tool image is mounted by name.
	MountRoot = "/opt/kagent/tools"

	pathEnvName = "PATH"
)

// MountPath returns the read-only mount directory of a named tool image.
func MountPath(name string) string {
	return MountRoot + "/" + name
}

// BinDir returns the PATH entry of a named tool image.
func BinDir(name string) string {
	return MountPath(name) + "/bin"
}

// JoinBinDirs returns the EnvName value for the named tool images, in order.
func JoinBinDirs(names []string) string {
	dirs := make([]string, 0, len(names))
	for _, name := range names {
		dirs = append(dirs, BinDir(name))
	}
	return strings.Join(dirs, ":")
}

// ExtendPath appends every non-empty entry of toolPaths that path does not
// already contain. Appending keeps the image's own binaries ahead of the tools.
func ExtendPath(path, toolPaths string) string {
	entries := strings.Split(path, ":")
	if path == "" {
		entries = nil
	}
	for dir := range strings.SplitSeq(toolPaths, ":") {
		if dir == "" || slices.Contains(entries, dir) {
			continue
		}
		entries = append(entries, dir)
	}
	return strings.Join(entries, ":")
}

// ProcessPath returns PATH extended by the EnvName entry of environment. The
// second result is false when environment carries no tool paths.
func ProcessPath(environment []string) (string, bool) {
	toolPaths, ok := lookup(environment, EnvName)
	if !ok || toolPaths == "" {
		return "", false
	}
	path, _ := lookup(environment, pathEnvName)
	return ExtendPath(path, toolPaths), true
}

// Extend returns environment with PATH extended by its own EnvName entry. It
// returns environment unchanged when no tool paths are set.
func Extend(environment []string) []string {
	path, ok := ProcessPath(environment)
	if !ok {
		return environment
	}
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if name, _, _ := strings.Cut(entry, "="); name == pathEnvName {
			continue
		}
		result = append(result, entry)
	}
	return append(result, pathEnvName+"="+path)
}

func lookup(environment []string, name string) (string, bool) {
	var value string
	found := false
	for _, entry := range environment {
		if key, entryValue, ok := strings.Cut(entry, "="); ok && key == name {
			value, found = entryValue, true
		}
	}
	return value, found
}
