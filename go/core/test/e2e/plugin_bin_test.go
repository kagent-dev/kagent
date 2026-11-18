package e2e_test

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

// TestOCIPluginBinOnPath verifies that the kagent runtime puts a mounted
// plugin's bin directory on PATH, so the bash tool runs its binary by bare
// name, and that a mounted plugin is not held to the fetched-artifact size cap.
func TestOCIPluginBinOnPath(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	for _, tt := range []struct {
		name  string
		large bool
	}{
		{name: "Given a mounted plugin with a bin directory, when the model runs its binary by bare name, assert that the output reaches the model"},
		{name: "Given a mounted plugin over 100 MiB and 10,000 files, when the model runs its binary by bare name, assert that the agent is ready and the output reaches the model", large: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			harness := testHarness{name: "kagent", runtimeLabel: "kagent"}
			image := publishPluginBinImage(t, tt.large)
			model := startModelRecorder(t, mockLLMProxy(t, startMockLLMServer(t, interactionMocks, "mocks/invoke_plugin_bin.json")), nil)
			kube := interactionKubeClient(t)
			config := harness.createModel(t, kube, reachableModelURL(t, model.URL), nil)
			template := &v1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "oci-plugin-bin-", Namespace: "kagent", Labels: harness.labels()},
				Spec: v1alpha3.AgentTemplateSpec{
					ModelConfig:  &v1alpha3.ObjectReference{Name: config.Name},
					SystemPrompt: "Run the requested plugin binary.",
					Plugins:      []v1alpha3.PluginBundle{{Source: v1alpha3.ArtifactSource{OCI: image}}},
				},
			}
			createAndWaitInteractionTemplate(t, harness, kube, template)
			fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)

			_, _, task := fixture.send(t, "Run the plugin binary.")

			require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, taskText(task))
			require.Contains(t, taskText(task), "PLUGIN_BIN_DONE")
			found := false
			for _, request := range model.Requests("", "") {
				found = found || bytes.Contains(request.Body, []byte("PLUGIN_BIN_OUTPUT"))
			}
			require.True(t, found, "plugin binary output did not reach the model")
		})
	}
}

// publishPluginBinImage pushes a plugin whose bin/kagent-e2e-plugin-tool prints
// PLUGIN_BIN_OUTPUT. The large variant adds more than 100 MiB and 10,000 files.
func publishPluginBinImage(t *testing.T, large bool) string {
	t.Helper()
	runtimeImage, err := name.NewDigest(kagentenv.E2ERuntimeImage.Get())
	require.NoError(t, err, "KAGENT_E2E_RUNTIME_IMAGE must name a digest-pinned image in the local test registry")
	tag := "small"
	if large {
		tag = "large"
	}
	ref := runtimeImage.Context().Registry.Repo("kagent-e2e/plugin-bin").Tag(tag)
	archivePath := filepath.Join(t.TempDir(), "layer.tar")
	archive, err := os.Create(archivePath)
	require.NoError(t, err)
	writer := tar.NewWriter(archive)
	writeFile := func(path, body string, mode int64) {
		require.NoError(t, writer.WriteHeader(&tar.Header{Name: path, Size: int64(len(body)), Mode: mode, Typeflag: tar.TypeReg}))
		_, err := io.WriteString(writer, body)
		require.NoError(t, err)
	}
	writeFile("plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"plugin-bin-test"}`, 0o644)
	writeFile("bin/kagent-e2e-plugin-tool", "#!/bin/sh\nprintf 'PLUGIN_BIN_%s\\n' OUTPUT\n", 0o755)
	if large {
		for i := range 10_001 {
			writeFile(fmt.Sprintf("share/file-%d", i), "", 0o644)
		}
		const size = 101 << 20
		require.NoError(t, writer.WriteHeader(&tar.Header{Name: "share/blob", Size: size, Mode: 0o644, Typeflag: tar.TypeReg}))
		_, err := io.CopyN(writer, zeroReader{}, size)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	require.NoError(t, archive.Close())
	layer, err := tarball.LayerFromFile(archivePath)
	require.NoError(t, err)
	image, err := mutate.AppendLayers(empty.Image, layer)
	require.NoError(t, err)
	require.NoError(t, remote.Write(ref, image, remote.WithContext(t.Context())))
	digest, err := image.Digest()
	require.NoError(t, err)
	return ref.Context().Digest(digest.String()).Name()
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
