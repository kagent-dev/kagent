package e2e_test

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

func TestOCISkillsSurviveSuspension(t *testing.T) {
	t.Parallel()
	target := interactionTarget(t)
	image := publishSkillImage(t)
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		root := harness.skillsRoot()
		model, setPhase := startMountedSkillsMock(t, root)
		kube := interactionKubeClient(t)
		config := harness.createModel(t, kube, reachableModelURL(t, model.URL), nil)
		template := &v1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "oci-skills-", Namespace: "kagent", Labels: harness.labels()},
			Spec: v1alpha3.AgentTemplateSpec{
				ModelConfig:  &corev1.LocalObjectReference{Name: config.Name},
				SystemPrompt: "Load the requested skills and follow their instructions.",
				Skills:       []v1alpha3.AgentTemplateSkill{{Name: "standalone", Source: v1alpha3.ArtifactSource{OCI: image, Path: "standalone"}}},
				Plugins:      []v1alpha3.PluginBundle{{Source: v1alpha3.ArtifactSource{OCI: image, Path: "plugin"}, Skills: []string{"selected"}}},
			},
		}
		// Ready requires the golden runtime to load the mounted package.
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		for _, phase := range []string{"initial", "restored"} {
			setPhase(phase)
			start := len(model.Requests("", ""))
			_, _, task := fixture.send(t, "Run mounted skills "+phase+".")
			require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, taskText(task))
			require.Contains(t, taskText(task), "SKILLS_"+phase+"_DONE")
			requests := slices.DeleteFunc(model.Requests("", "")[start:], func(r recordedModelRequest) bool { return len(r.Body) == 0 })
			require.NotEmpty(t, requests)
			for _, description := range []string{"Execute the standalone mounted skill.", "Execute the selected mounted skill."} {
				require.Contains(t, string(requests[0].Body), description, "skill must be advertised to the model")
			}
			require.NotContains(t, string(requests[0].Body), "This skill must not be installed.")
			if phase == "initial" {
				require.NotContains(t, string(requests[0].Body), "SKILL_BODY_", "discovery should expose metadata, not full instructions")
			}
			// Only the newest input can prove that this turn loaded instructions
			// and executed scripts; restored conversation history cannot satisfy it.
			for _, want := range []string{
				"SKILL_BODY_STANDALONE", "SKILL_BODY_SELECTED",
				"mounted-standalone-volume-" + phase + ":mounted-standalone-" + phase + ":mounted-selected-" + phase,
			} {
				found := false
				for _, request := range requests[1:] {
					var body struct {
						Messages []json.RawMessage `json:"messages"`
						Input    []json.RawMessage `json:"input"`
					}
					require.NoError(t, json.Unmarshal(request.Body, &body))
					messages := append(body.Messages, body.Input...)
					if len(messages) > 0 {
						found = found || bytes.Contains(messages[len(messages)-1], []byte(want))
					}
				}
				require.True(t, found, "model did not receive fresh skill instructions/output %q during %s", want, phase)
			}
			assertActorSuspended(t, fixture)
		}
	})
}

// Publish a small normal container image to the same local registry used by
// runtime fixtures. Substrate's worker registry rewrite applies to both.
func publishSkillImage(t *testing.T) string {
	t.Helper()
	runtimeImage, err := name.NewDigest(kagentenv.E2ERuntimeImage.Get())
	require.NoError(t, err, "KAGENT_E2E_RUNTIME_IMAGE must name a digest-pinned image in the local test registry")
	ref := runtimeImage.Context().Registry.Repo("kagent-e2e/skills").Tag("mounted")
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, file := range []struct {
		path string
		body string
		mode int64
	}{
		{"egress/SKILL.md", "---\nname: egress\ndescription: Probe live HTTP egress permissions.\n---\nSKILL_BODY_EGRESS: Run probe.sh against the requested control and destination URLs.\n", 0o644},
		{"egress/probe.sh", egressProbeScript, 0o755},
		{"standalone/SKILL.md", "---\nname: standalone\ndescription: Execute the standalone mounted skill.\n---\nSKILL_BODY_STANDALONE: Run run.sh with the requested phase.\n", 0o644},
		{"standalone/run.sh", "#!/bin/sh\nprintf 'mounted-standalone-%s:' \"$1\"\n", 0o755},
		{"plugin/plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"mounted-test"}`, 0o644},
		{"plugin/skills/selected/SKILL.md", "---\nname: selected\ndescription: Execute the selected mounted skill.\n---\nSKILL_BODY_SELECTED: Run run.sh with the requested phase.\n", 0o644},
		{"plugin/skills/selected/run.sh", "#!/bin/sh\nprintf 'mounted-selected-%s' \"$1\"\n", 0o755},
		{"plugin/skills/disabled/SKILL.md", "---\nname: disabled\ndescription: This skill must not be installed.\n---\nDisabled.\n", 0o644},
	} {
		require.NoError(t, writer.WriteHeader(&tar.Header{Name: file.path, Size: int64(len(file.body)), Mode: file.mode, Typeflag: tar.TypeReg}))
		_, err := io.WriteString(writer, file.body)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(archive.Bytes())), nil
	})
	require.NoError(t, err)
	image, err := mutate.AppendLayers(empty.Image, layer)
	require.NoError(t, err)
	require.NoError(t, remote.Write(ref, image, remote.WithContext(t.Context())))
	digest, err := image.Digest()
	require.NoError(t, err)
	return ref.Context().Digest(digest.String()).Name()
}

func startMountedSkillsMock(t *testing.T, root string) (*modelRecorder, func(string)) {
	t.Helper()
	fixture, err := interactionMocks.ReadFile("mocks/invoke_mounted_skills.json")
	require.NoError(t, err)
	// Loading a skill returns the same instructions in both turns. Keep each
	// phase's mock separate so identical tool output cannot match the old turn.
	proxies := make(map[string]*httputil.ReverseProxy)
	for _, phase := range []string{"initial", "restored"} {
		command := fmt.Sprintf("test ! -e %s/disabled && /run/kagent/artifacts/*/standalone/run.sh volume-%s && %s/standalone/run.sh %s && %s/selected/run.sh %s", root, phase, root, phase, root, phase)
		args, err := json.Marshal(struct {
			Command string `json:"command"`
		}{command})
		require.NoError(t, err)
		encodedArgs, err := json.Marshal(string(args))
		require.NoError(t, err)
		codexArgs, err := json.Marshal(struct {
			Command string `json:"cmd"`
		}{command})
		require.NoError(t, err)
		encodedCodexArgs, err := json.Marshal(string(codexArgs))
		require.NoError(t, err)
		encodedCommand, err := json.Marshal(command)
		require.NoError(t, err)
		data := strings.NewReplacer("PHASE", phase, "ROOT", root, `"COMMAND_ARGUMENTS"`, string(encodedArgs), `"CODEX_COMMAND_ARGUMENTS"`, string(encodedCodexArgs), `"COMMAND"`, string(encodedCommand)).Replace(string(fixture))
		var part mockllm.Config
		require.NoError(t, json.Unmarshal([]byte(data), &part))
		upstream, err := url.Parse(startMockLLMConfig(t, part))
		require.NoError(t, err)
		proxies[phase] = httputil.NewSingleHostReverseProxy(upstream)
	}
	var active atomic.Pointer[httputil.ReverseProxy]
	active.Store(proxies["initial"])
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Load().ServeHTTP(w, r)
	})
	return startModelRecorder(t, upstream, nil), func(phase string) { active.Store(proxies[phase]) }
}
