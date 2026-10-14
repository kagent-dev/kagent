package toolpath

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJoinBinDirsKeepsListOrder(t *testing.T) {
	require.Equal(t, "/opt/kagent/tools/kubectl/bin:/opt/kagent/tools/helm/bin", JoinBinDirs([]string{"kubectl", "helm"}))
	require.Equal(t, "", JoinBinDirs(nil))
}

func TestExtendPathAppendsAfterImagePath(t *testing.T) {
	got := ExtendPath("/usr/local/bin:/usr/bin", "/opt/kagent/tools/go/bin:/opt/kagent/tools/kubectl/bin")
	require.Equal(t, "/usr/local/bin:/usr/bin:/opt/kagent/tools/go/bin:/opt/kagent/tools/kubectl/bin", got)
}

func TestExtendPathSkipsEmptyAndDuplicateEntries(t *testing.T) {
	got := ExtendPath("/usr/bin:/opt/kagent/tools/go/bin", ":/opt/kagent/tools/go/bin::/opt/kagent/tools/helm/bin:")
	require.Equal(t, "/usr/bin:/opt/kagent/tools/go/bin:/opt/kagent/tools/helm/bin", got)
}

func TestExtendPathStartsFromEmptyPath(t *testing.T) {
	require.Equal(t, "/opt/kagent/tools/go/bin", ExtendPath("", "/opt/kagent/tools/go/bin"))
}

func TestExtendIsNoopWithoutToolPaths(t *testing.T) {
	environment := []string{"PATH=/bin", "HOME=/data"}
	require.Equal(t, environment, Extend(environment))
	require.Equal(t, []string{"PATH=/bin", "KAGENT_TOOL_PATHS="}, Extend([]string{"PATH=/bin", "KAGENT_TOOL_PATHS="}))
}

func TestExtendReplacesPathAndKeepsToolPaths(t *testing.T) {
	got := Extend([]string{"PATH=/bin", "KAGENT_TOOL_PATHS=/opt/kagent/tools/go/bin", "HOME=/data", "PATH=/usr/bin"})
	require.Equal(t, []string{"KAGENT_TOOL_PATHS=/opt/kagent/tools/go/bin", "HOME=/data", "PATH=/usr/bin:/opt/kagent/tools/go/bin"}, got)
}

func TestExtendAddsPathWhenImageHasNone(t *testing.T) {
	got := Extend([]string{"KAGENT_TOOL_PATHS=/opt/kagent/tools/go/bin"})
	require.Equal(t, []string{"KAGENT_TOOL_PATHS=/opt/kagent/tools/go/bin", "PATH=/opt/kagent/tools/go/bin"}, got)
}

func TestProcessPath(t *testing.T) {
	path, ok := ProcessPath([]string{"PATH=/bin", "KAGENT_TOOL_PATHS=/opt/kagent/tools/go/bin"})
	require.True(t, ok)
	require.Equal(t, "/bin:/opt/kagent/tools/go/bin", path)
	_, ok = ProcessPath([]string{"PATH=/bin"})
	require.False(t, ok)
}
