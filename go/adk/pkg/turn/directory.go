// Package turn holds what the runtime knows about the turn a call belongs to.
package turn

import "context"

type directoryKey struct{}

// WithDirectory returns a context whose turn works in dir: the directory the
// turn's dispatch names, which the runtime learns per turn and never at boot,
// since a runtime restored from a snapshot predates the session's mounts.
func WithDirectory(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, directoryKey{}, dir)
}

// Directory returns the directory the turn ctx belongs to works in, or empty
// when its dispatch named none.
func Directory(ctx context.Context) string {
	dir, _ := ctx.Value(directoryKey{}).(string)
	return dir
}
