package e2e_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The workspace fixture: one read-write-many volume of the NFS class
// (scripts/kind/setup-csi-nfs.sh) holding a bare mirror of each repository
// under mirrors/ and an empty sessions/, the origin those mirrors were fetched
// from, and the volume's CSI driver and handle, which a Session volume source
// names.
const (
	workspaceStorageClass = "csi-nfs-sc"
	workspaceDriver       = "nfs.csi.k8s.io"
	// workspaceGitImage writes the volume and reads it back; the Kind node
	// pulls it from Docker Hub.
	workspaceGitImage = "docker.io/alpine/git:2.54.0"
	// workspaceGitToken is the origin's credential, as a provider token the
	// egress gateway would set: the volume and a Session's sandbox never hold it.
	workspaceGitToken = "workspace-e2e-token"
	// workspaceClaimBindTimeout bounds the wait for the fixture's claim to
	// bind; the provisioner mounts the NFS server to create its directory.
	workspaceClaimBindTimeout = 3 * time.Minute
	// The in-cluster NFS server the StorageClass provisions from.
	workspaceNFSServerNamespace = "default"
	workspaceNFSServerApp       = "nfs-server"
	// workspaceSendRetryWindow bounds the resends of a refused turn on a
	// Session with a workspace volume: its Actor's first resume mounts the
	// NFS volume, which Substrate retries after a mount timeout, so the
	// Session can take longer than sendRetryWindow to become available.
	workspaceSendRetryWindow = 3 * time.Minute
)

var (
	// workspaceRepositories are mirrored under mirrors/<owner>/<repository>.git.
	workspaceRepositories = []string{"e2e/alpha", "e2e/beta"}
	// workspaceChanged is pushed to after the mirrors were fetched.
	workspaceChanged = "e2e/beta"
)

// workspaceVolume names an existing CSI volume by its driver and handle.
type workspaceVolume struct{ Driver, Handle string }

// workspaceFixture is a read-write-many volume holding a bare mirror of every
// workspaceRepositories origin and an empty sessions/ directory, with the
// origin it was fetched from. Origin moved on workspaceChanged after the fetch.
type workspaceFixture struct {
	// Volume is what a grant names and a Session volume source carries.
	Volume workspaceVolume
	// Claim is the PersistentVolumeClaim in namespace kagent.
	Claim *corev1.PersistentVolumeClaim
	// Heads maps each repository to the commit its mirror's main holds.
	Heads map[string]string
	// Changed lists the repositories whose origin moved after the fetch.
	Changed []string
	// Origin serves the repositories as "<owner>/<repository>.git" under
	// OriginURL, over HTTPS for the egress gateway, and records every request.
	Origin    *gitFixture
	OriginURL string
	// Authorization is the header value Origin requires; empty when it
	// serves anyone.
	Authorization string
}

func newWorkspaceFixture(t *testing.T) *workspaceFixture {
	t.Helper()
	return newWorkspaceFixtureOf(t, true)
}

// newWorkspaceFixtureOf builds the fixture; an origin without a credential
// serves anyone, as a public repository does.
func newWorkspaceFixtureOf(t *testing.T, credentialed bool) *workspaceFixture {
	t.Helper()
	var authorization string
	if credentialed {
		authorization = "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+workspaceGitToken))
	}
	origin := newGitFixtureOf(t, authorization, workspaceOriginPaths()...)
	return newWorkspaceFixtureFrom(t, origin, origin.serveThroughEgress(t), authorization)
}

// newWorkspaceFixtureFrom mirrors the repositories of origin, served at
// originURL, onto a new workspace volume, fetching them with authorization,
// a Basic credential or none.
func newWorkspaceFixtureFrom(t *testing.T, origin *gitFixture, originURL, authorization string) *workspaceFixture {
	t.Helper()
	kube := interactionKubeClient(t)
	fixture := &workspaceFixture{Heads: map[string]string{}, Changed: []string{workspaceChanged}, Origin: origin, OriginURL: originURL, Authorization: authorization}
	env := []corev1.EnvVar{{Name: "GIT_SSL_CAINFO", Value: "/origin-ca/ca.crt"}, {Name: "ORIGIN", Value: originURL}}
	if credential, basic := strings.CutPrefix(authorization, "Basic "); basic {
		secret := createCredentialSecret(t, kube, credential)
		env = append(env, corev1.EnvVar{Name: "GIT_CREDENTIAL", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token"}}})
	}

	authority, err := os.ReadFile(kagentenv.E2EEgressCAFile.Get())
	require.NoError(t, err)
	trust := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-origin-ca-", Namespace: "kagent"}, Data: map[string]string{"ca.crt": string(authority)}}
	createWorkspaceObject(t, kube, trust)

	fixture.Claim, fixture.Volume = createWorkspaceVolume(t, kube)
	mirror := `set -eu
mkdir -p /volume/sessions
set --
if [ -n "${GIT_CREDENTIAL:-}" ]; then set -- -c "http.extraHeader=Authorization: Basic $GIT_CREDENTIAL"; fi
for repository in ` + strings.Join(workspaceRepositories, " ") + `; do
  git "$@" clone --quiet --mirror "$ORIGIN/$repository.git" "/volume/mirrors/$repository.git"
done
` + workspaceMirrorHeadsScript("/volume/mirrors")
	report := runWorkspaceJob(t, kube, fixture.Claim, mirror,
		[]workspaceMount{{Path: "/volume"}}, env,
		corev1.Volume{Name: "origin-ca", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: trust.Name}}}},
	)
	for repository, head := range parseWorkspaceHeads(t, report) {
		require.Equal(t, fixture.Origin.head(t, repository+".git"), head, "%s is mirrored at its origin's head", repository)
		fixture.Heads[repository] = head
	}
	require.Len(t, fixture.Heads, len(workspaceRepositories))
	fixture.Origin.push(t, workspaceChanged+".git", "CHANGED.md", "Pushed after the workspace mirrors were fetched.\n")
	return fixture
}

func workspaceOriginPaths() []string {
	paths := make([]string, 0, len(workspaceRepositories))
	for _, repository := range workspaceRepositories {
		paths = append(paths, repository+".git")
	}
	return paths
}

// workspaceMirrorHeadsScript reports "<repository> <commit>" of every mirror's
// main under mirrors in the termination message.
func workspaceMirrorHeadsScript(mirrors string) string {
	return `for repository in ` + strings.Join(workspaceRepositories, " ") + `; do
  printf '%s %s\n' "$repository" "$(git -C "` + mirrors + `/$repository.git" rev-parse refs/heads/main)" >> /dev/termination-log
done
`
}

func parseWorkspaceHeads(t *testing.T, report string) map[string]string {
	t.Helper()
	heads := map[string]string{}
	for line := range strings.Lines(strings.TrimSpace(report)) {
		fields := strings.Fields(line)
		require.Len(t, fields, 2, "workspace report line %q", line)
		heads[fields[0]] = fields[1]
	}
	return heads
}

// createWorkspaceVolume claims a read-write-many volume of the NFS class,
// waits until it is bound, and reads the CSI driver and volume handle from its
// PersistentVolume.
func createWorkspaceVolume(t *testing.T, kube ctrlclient.Client) (*corev1.PersistentVolumeClaim, workspaceVolume) {
	t.Helper()
	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-", Namespace: "kagent"},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: new(workspaceStorageClass),
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
		},
	}
	createWorkspaceObject(t, kube, claim)
	current := &corev1.PersistentVolumeClaim{}
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, workspaceClaimBindTimeout, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(claim), current); err != nil {
			return false, err
		}
		return current.Status.Phase == corev1.ClaimBound, nil
	})
	if err != nil {
		t.Fatalf("%v: %s", err, unboundClaimReport(t, kube, current))
	}
	var volume corev1.PersistentVolume
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Name: current.Spec.VolumeName}, &volume))
	require.NotNil(t, volume.Spec.CSI, "PersistentVolume %s has a CSI source", volume.Name)
	require.NotEmpty(t, volume.Spec.CSI.VolumeHandle)
	return claim, workspaceVolume{Driver: volume.Spec.CSI.Driver, Handle: volume.Spec.CSI.VolumeHandle}
}

// unboundClaimReport says why claim is not bound: its phase, the events of its
// provisioning, which carry the provisioner's error, and the state of the NFS
// server behind the StorageClass.
func unboundClaimReport(t *testing.T, kube ctrlclient.Client, claim *corev1.PersistentVolumeClaim) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
	defer cancel()
	var events corev1.EventList
	eventsErr := kube.List(ctx, &events, ctrlclient.InNamespace(claim.Namespace))
	var servers corev1.PodList
	serversErr := kube.List(ctx, &servers, ctrlclient.InNamespace(workspaceNFSServerNamespace), ctrlclient.MatchingLabels{"app": workspaceNFSServerApp})
	report := fmt.Sprintf("claim %s/%s of StorageClass %s is %s after %s; its events: ", claim.Namespace, claim.Name, workspaceStorageClass, claim.Status.Phase, workspaceClaimBindTimeout)
	if eventsErr != nil {
		report += "unreadable: " + eventsErr.Error()
	} else {
		report += objectEvents(events.Items, "PersistentVolumeClaim", claim.Name)
	}
	report += "; NFS server: "
	if serversErr != nil {
		return report + "unreadable: " + serversErr.Error()
	}
	return report + podStates(servers.Items)
}

// objectEvents lists the events of the named object, oldest first, as
// "Reason: message" with their count, or says there are none.
func objectEvents(events []corev1.Event, kind, name string) string {
	var own []corev1.Event
	for _, event := range events {
		if event.InvolvedObject.Kind == kind && event.InvolvedObject.Name == name {
			own = append(own, event)
		}
	}
	if len(own) == 0 {
		return "none"
	}
	slices.SortStableFunc(own, func(a, b corev1.Event) int { return eventTime(a).Compare(eventTime(b)) })
	lines := make([]string, 0, len(own))
	for _, event := range own {
		line := event.Reason + ": " + event.Message
		if event.Count > 1 {
			line += fmt.Sprintf(" (x%d)", event.Count)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "; ")
}

func eventTime(event corev1.Event) time.Time {
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	return event.CreationTimestamp.Time
}

// podStates describes each pod as "<name> <phase>, ready <bool>, <n>
// restarts", or says there is none.
func podStates(pods []corev1.Pod) string {
	if len(pods) == 0 {
		return "no pod"
	}
	states := make([]string, 0, len(pods))
	for _, pod := range pods {
		ready, restarts := false, int32(0)
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady {
				ready = condition.Status == corev1.ConditionTrue
			}
		}
		for _, container := range pod.Status.ContainerStatuses {
			restarts += container.RestartCount
		}
		states = append(states, fmt.Sprintf("%s %s, ready %t, %d restarts", pod.Name, pod.Status.Phase, ready, restarts))
	}
	return strings.Join(states, "; ")
}

// workspaceMount mounts SubPath of the workspace volume at Path.
type workspaceMount struct {
	Path, SubPath string
	ReadOnly      bool
}

// runWorkspaceJob runs script in the git image with the workspace volume's
// mounts and every extra volume read-only at /<name>, and returns what it
// wrote to /dev/termination-log.
func runWorkspaceJob(t *testing.T, kube ctrlclient.Client, claim *corev1.PersistentVolumeClaim, script string, workspace []workspaceMount, env []corev1.EnvVar, volumes ...corev1.Volume) string {
	t.Helper()
	mounts := make([]corev1.VolumeMount, 0, len(workspace)+len(volumes))
	for _, mount := range workspace {
		mounts = append(mounts, corev1.VolumeMount{Name: "workspace", MountPath: mount.Path, SubPath: mount.SubPath, ReadOnly: mount.ReadOnly})
	}
	for _, volume := range volumes {
		mounts = append(mounts, corev1.VolumeMount{Name: volume.Name, MountPath: "/" + volume.Name, ReadOnly: true})
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-", Namespace: "kagent"},
		Spec: batchv1.JobSpec{
			BackoffLimit: new(int32(0)),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{{
					Name: "git", Image: workspaceGitImage, Command: []string{"sh", "-c", script}, Env: env, VolumeMounts: mounts,
					TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
				}},
				Volumes: append([]corev1.Volume{{Name: "workspace", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim.Name},
				}}}, volumes...),
			}},
		},
	}
	createWorkspaceObject(t, kube, job)
	var pods corev1.PodList
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		pods = corev1.PodList{}
		if !assert.NoError(c, kube.List(t.Context(), &pods, ctrlclient.InNamespace(job.Namespace), ctrlclient.MatchingLabels{batchv1.JobNameLabel: job.Name})) || !assert.Len(c, pods.Items, 1) {
			return
		}
		phase := pods.Items[0].Status.Phase
		assert.Contains(c, []corev1.PodPhase{corev1.PodSucceeded, corev1.PodFailed}, phase, "workspace job %s pod phase", job.Name)
	}, 5*time.Minute, 2*time.Second)
	pod := pods.Items[0]
	var message string
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Terminated != nil {
			message = status.State.Terminated.Message
		}
	}
	require.Equal(t, corev1.PodSucceeded, pod.Status.Phase, "workspace job %s: %s", job.Name, message)
	return message
}

func createWorkspaceObject(t *testing.T, kube ctrlclient.Client, object ctrlclient.Object) {
	t.Helper()
	require.NoError(t, kube.Create(t.Context(), object))
	t.Cleanup(func() {
		err := kube.Delete(context.Background(), object, ctrlclient.PropagationPolicy(metav1.DeletePropagationBackground))
		if err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete %T %s: %v", object, object.GetName(), err)
		}
	})
}

// TestWorkspaceVolumeFixture proves the fixture is the workspace the plan's
// storage model describes: one read-write-many volume whose mirrors a session
// directory clones from with alternates, each session writing only its own
// directory and none the mirrors, and no clone keeping the origin's credential.
func TestWorkspaceVolumeFixture(t *testing.T) {
	t.Parallel()
	interactionTarget(t)
	fixture := newWorkspaceFixture(t)
	require.Equal(t, workspaceDriver, fixture.Volume.Driver)
	t.Logf("workspace volume %s: driver %s, handle %s, heads %v", fixture.Claim.Name, fixture.Volume.Driver, fixture.Volume.Handle, fixture.Heads)

	kube := interactionKubeClient(t)
	for _, session := range []string{"a", "b"} {
		script := `set -eu
for repository in ` + strings.Join(workspaceRepositories, " ") + `; do
  git clone --quiet --shared --no-checkout "/mirrors/$repository.git" "/workspace/$repository"
  git -C "/workspace/$repository" switch --quiet main
done
echo ` + session + ` > /workspace/SESSION
test "$(ls /workspace)" = "$(printf 'SESSION
e2e')"
! touch /mirrors/written 2>/dev/null
` + workspaceMirrorHeadsScript("/mirrors") + `! grep -rlF -e "$GIT_TOKEN" /workspace >&2`
		report := runWorkspaceJob(t, kube, fixture.Claim, script,
			[]workspaceMount{{Path: "/workspace", SubPath: "sessions/" + session}, {Path: "/mirrors", SubPath: "mirrors", ReadOnly: true}},
			[]corev1.EnvVar{{Name: "GIT_TOKEN", Value: workspaceGitToken}})
		require.Equal(t, fixture.Heads, parseWorkspaceHeads(t, report), "session %s reads the mirrors", session)
	}

	// The origin moved on the changed repository only.
	for _, repository := range workspaceRepositories {
		moved := fixture.Origin.head(t, repository+".git") != fixture.Heads[repository]
		require.Equal(t, repository == workspaceChanged, moved, "origin of %s moved after the fetch", repository)
	}
}

// sandboxShell runs a POSIX shell script in a Session's sandbox and returns
// its standard output.
type sandboxShell func(t *testing.T, script string) string

// requireNoTokenInSandbox fails when token is in the sandbox's environment or
// in a file under any of paths (default /data and /workspace, the durable
// directory and the clone), or when one of paths is missing.
func requireNoTokenInSandbox(t *testing.T, shell sandboxShell, token string, paths ...string) {
	t.Helper()
	require.NoError(t, sandboxTokenViolation(t, shell, token, paths...))
}

func sandboxTokenViolation(t *testing.T, shell sandboxShell, token string, paths ...string) error {
	t.Helper()
	if len(paths) == 0 {
		paths = []string{"/data", "/workspace"}
	}
	if strings.Contains(shell(t, "env"), token) {
		return errors.New("the sandbox environment carries the token")
	}
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, shellQuote(path))
	}
	script := `for path in ` + strings.Join(quoted, " ") + `; do
  if [ ! -e "$path" ]; then echo "missing $path"; continue; fi
  grep -rlF -e ` + shellQuote(token) + ` -- "$path" 2>/dev/null || true
done`
	if found := strings.TrimSpace(shell(t, script)); found != "" {
		return fmt.Errorf("the sandbox holds the token or misses a path: %s", strings.ReplaceAll(found, "\n", ", "))
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// requireNoOriginRequest fails when the origin logged a request for
// repository, such as a fetch of a repository that did not change.
func requireNoOriginRequest(t *testing.T, origin *gitFixture, repository string) {
	t.Helper()
	received := origin.received(repository + ".git")
	require.Empty(t, received, "the origin logged %d requests for %s", len(received), repository)
}

func TestSandboxTokenViolation(t *testing.T) {
	const token = "sandbox-token-check"
	hostShell := func(environment ...string) sandboxShell {
		return func(t *testing.T, script string) string {
			command := exec.CommandContext(t.Context(), "sh", "-c", script)
			command.Env = append(os.Environ(), environment...)
			output, err := command.Output()
			require.NoError(t, err)
			return string(output)
		}
	}
	clean, leaked := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(clean, "config"), []byte("[remote \"origin\"]\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(leaked, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(leaked, ".git", "config"), []byte("extraheader = Basic "+token+"\n"), 0o644))
	for _, test := range []struct {
		name    string
		shell   sandboxShell
		paths   []string
		wantErr string
	}{
		{name: "clean", shell: hostShell(), paths: []string{clean}},
		{name: "environment", shell: hostShell("GITHUB_TOKEN=" + token), paths: []string{clean}, wantErr: "environment"},
		{name: "file", shell: hostShell(), paths: []string{clean, leaked}, wantErr: filepath.Join(leaked, ".git", "config")},
		{name: "missing path", shell: hostShell(), paths: []string{filepath.Join(clean, "absent")}, wantErr: "missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.wantErr == "" {
				requireNoTokenInSandbox(t, test.shell, token, test.paths...)
				return
			}
			require.ErrorContains(t, sandboxTokenViolation(t, test.shell, token, test.paths...), test.wantErr)
		})
	}
}

func TestGitRepositoryOf(t *testing.T) {
	for path, want := range map[string]string{
		"/skill.git/info/refs":           "skill.git",
		"/e2e/alpha.git/git-upload-pack": "e2e/alpha.git",
		"/e2e/alpha.git":                 "e2e/alpha.git",
		"/e2e/alpha.github/info/refs":    "",
		"/unknown":                       "",
	} {
		require.Equal(t, want, gitRepositoryOf(path), path)
	}
}

func TestGitFixturePush(t *testing.T) {
	fixture := newGitFixtureOf(t, "Basic unused", workspaceOriginPaths()...)
	before := fixture.head(t, "e2e/alpha.git")
	require.Equal(t, fixture.commit, before)
	after := fixture.push(t, "e2e/alpha.git", "CHANGED.md", "moved\n")
	require.NotEqual(t, before, after)
	require.Equal(t, fixture.commit, fixture.head(t, "e2e/beta.git"))
	requireNoOriginRequest(t, fixture, "e2e/alpha")
}

// A claim that stays Pending is reported with its provisioning events, oldest
// first, and the NFS server's state; another object's events are left out.
func TestUnboundClaimReportParts(t *testing.T) {
	at := func(seconds int) metav1.Time { return metav1.NewTime(time.Unix(int64(seconds), 0)) }
	claimEvent := func(reason, message string, seconds int, count int32) corev1.Event {
		return corev1.Event{
			InvolvedObject: corev1.ObjectReference{Kind: "PersistentVolumeClaim", Name: "workspace-1"},
			Reason:         reason, Message: message, LastTimestamp: at(seconds), Count: count,
		}
	}
	events := []corev1.Event{
		claimEvent("ProvisioningFailed", "failed to mount nfs server: timeout after 110s", 20, 2),
		{InvolvedObject: corev1.ObjectReference{Kind: "PersistentVolumeClaim", Name: "workspace-2"}, Reason: "ProvisioningSucceeded", LastTimestamp: at(5)},
		claimEvent("Provisioning", "External provisioner is provisioning volume", 10, 1),
	}
	require.Equal(t,
		"Provisioning: External provisioner is provisioning volume; ProvisioningFailed: failed to mount nfs server: timeout after 110s (x2)",
		objectEvents(events, "PersistentVolumeClaim", "workspace-1"))
	require.Equal(t, "none", objectEvents(events, "PersistentVolumeClaim", "workspace-3"))

	require.Equal(t, "no pod", podStates(nil))
	server := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "nfs-server-1"},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{RestartCount: 1}},
		},
	}
	require.Equal(t, "nfs-server-1 Running, ready true, 1 restarts", podStates([]corev1.Pod{server}))
}
