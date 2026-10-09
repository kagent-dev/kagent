package e2e_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/test/grant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The workspace fixture: a volume holding git clones, snapshotted through the
// CSI hostpath driver (scripts/kind/setup-csi-snapshots.sh), the snapshot a
// Session's clone is seeded from, and the origin those clones fetch from.
const (
	workspaceStorageClass   = "csi-hostpath-sc"
	workspaceSnapshotClass  = "csi-hostpath-snapclass"
	workspaceSnapshotDriver = "hostpath.csi.k8s.io"
	// workspaceGitImage clones into the volume and reads it back; the Kind node
	// pulls it from Docker Hub.
	workspaceGitImage = "docker.io/alpine/git:2.54.0"
	// workspaceGitToken is the origin's credential, as a provider token the
	// egress gateway would set: the volume and a Session's sandbox never hold it.
	workspaceGitToken = "workspace-e2e-token"
)

var (
	// workspaceRepositories are cloned under /<owner>/<repository> of the volume.
	workspaceRepositories = []string{"e2e/alpha", "e2e/beta"}
	// workspaceChanged is pushed to after the snapshot.
	workspaceChanged = "e2e/beta"
	snapshotGVK      = schema.GroupVersionKind{Group: "snapshot.storage.k8s.io", Version: "v1", Kind: "VolumeSnapshot"}
	snapshotContent  = schema.GroupVersionKind{Group: "snapshot.storage.k8s.io", Version: "v1", Kind: "VolumeSnapshotContent"}
)

// workspaceFixture is a ready VolumeSnapshot of a volume holding every
// workspaceRepositories clone, checked out on main, with the origin it was
// cloned from. Origin moved on workspaceChanged after the snapshot.
type workspaceFixture struct {
	// Snapshot is what a grant names and a Session volume source carries.
	Snapshot grant.Snapshot
	// SnapshotName is the VolumeSnapshot in namespace kagent.
	SnapshotName string
	// Heads maps each repository to the commit its clone holds.
	Heads map[string]string
	// Changed lists the repositories whose origin moved after the snapshot.
	Changed []string
	// Origin serves the repositories as "<owner>/<repository>.git" under
	// OriginURL, over HTTPS for the egress gateway, and records every request.
	Origin    *gitFixture
	OriginURL string
	// Authorization is the header value Origin requires.
	Authorization string
}

func newWorkspaceFixture(t *testing.T) *workspaceFixture {
	t.Helper()
	kube := interactionKubeClient(t)
	credential := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + workspaceGitToken))
	fixture := &workspaceFixture{Authorization: "Basic " + credential, Heads: map[string]string{}, Changed: []string{workspaceChanged}}
	fixture.Origin = newGitFixtureOf(t, fixture.Authorization, workspaceOriginPaths()...)
	fixture.OriginURL = fixture.Origin.serveThroughEgress(t)

	secret := createCredentialSecret(t, kube, credential)
	authority, err := os.ReadFile(kagentenv.E2EEgressCAFile.Get())
	require.NoError(t, err)
	trust := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-origin-ca-", Namespace: "kagent"}, Data: map[string]string{"ca.crt": string(authority)}}
	createWorkspaceObject(t, kube, trust)

	volume := createWorkspaceVolume(t, kube, nil)
	clone := `set -eu
for repository in ` + strings.Join(workspaceRepositories, " ") + `; do
  git -c "http.extraHeader=Authorization: Basic $GIT_CREDENTIAL" clone --quiet "$ORIGIN/$repository.git" "/workspace/$repository"
done
` + workspaceHeadsScript
	report := runWorkspaceJob(t, kube, volume, clone,
		[]corev1.EnvVar{
			{Name: "ORIGIN", Value: fixture.OriginURL},
			{Name: "GIT_SSL_CAINFO", Value: "/origin-ca/ca.crt"},
			{Name: "GIT_CREDENTIAL", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}, Key: "token"}}},
		},
		corev1.Volume{Name: "origin-ca", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: trust.Name}}}},
	)
	for repository, checkout := range parseWorkspaceHeads(t, report) {
		require.Equal(t, "main", checkout.branch, "%s is checked out on its default branch", repository)
		require.Equal(t, fixture.Origin.head(t, repository+".git"), checkout.commit, "%s is cloned at its origin's head", repository)
		fixture.Heads[repository] = checkout.commit
	}
	require.Len(t, fixture.Heads, len(workspaceRepositories))

	fixture.SnapshotName, fixture.Snapshot = snapshotWorkspaceVolume(t, kube, volume)
	fixture.Origin.push(t, workspaceChanged+".git", "CHANGED.md", "Pushed after the workspace snapshot.\n")
	return fixture
}

func workspaceOriginPaths() []string {
	paths := make([]string, 0, len(workspaceRepositories))
	for _, repository := range workspaceRepositories {
		paths = append(paths, repository+".git")
	}
	return paths
}

// workspaceHeadsScript reports "<repository> <branch> <commit>" for every clone
// in the termination message.
var workspaceHeadsScript = `for repository in ` + strings.Join(workspaceRepositories, " ") + `; do
  printf '%s %s %s\n' "$repository" "$(git -C "/workspace/$repository" symbolic-ref --short HEAD)" "$(git -C "/workspace/$repository" rev-parse HEAD)" >> /dev/termination-log
done
`

type workspaceCheckout struct{ branch, commit string }

func parseWorkspaceHeads(t *testing.T, report string) map[string]workspaceCheckout {
	t.Helper()
	heads := map[string]workspaceCheckout{}
	for line := range strings.Lines(strings.TrimSpace(report)) {
		fields := strings.Fields(line)
		require.Len(t, fields, 3, "workspace report line %q", line)
		heads[fields[0]] = workspaceCheckout{branch: fields[1], commit: fields[2]}
	}
	return heads
}

// createWorkspaceVolume provisions a volume on the hostpath driver, empty or
// restored from a VolumeSnapshot.
func createWorkspaceVolume(t *testing.T, kube ctrlclient.Client, snapshot *string) *corev1.PersistentVolumeClaim {
	t.Helper()
	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "workspace-", Namespace: "kagent"},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: new(workspaceStorageClass),
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
		},
	}
	if snapshot != nil {
		claim.Spec.DataSource = &corev1.TypedLocalObjectReference{APIGroup: new(snapshotGVK.Group), Kind: snapshotGVK.Kind, Name: *snapshot}
	}
	createWorkspaceObject(t, kube, claim)
	return claim
}

// runWorkspaceJob runs script in the git image with the volume at /workspace
// and every extra volume at /<name>, and returns what it wrote to
// /dev/termination-log.
func runWorkspaceJob(t *testing.T, kube ctrlclient.Client, claim *corev1.PersistentVolumeClaim, script string, env []corev1.EnvVar, volumes ...corev1.Volume) string {
	t.Helper()
	mounts := []corev1.VolumeMount{{Name: "workspace", MountPath: "/workspace"}}
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

// snapshotWorkspaceVolume takes a VolumeSnapshot of claim, waits until it is
// ready to use, and reads the CSI driver and snapshot handle from its bound
// VolumeSnapshotContent.
func snapshotWorkspaceVolume(t *testing.T, kube ctrlclient.Client, claim *corev1.PersistentVolumeClaim) (string, grant.Snapshot) {
	t.Helper()
	snapshot := &unstructured.Unstructured{}
	snapshot.SetGroupVersionKind(snapshotGVK)
	snapshot.SetGenerateName("workspace-")
	snapshot.SetNamespace(claim.Namespace)
	require.NoError(t, unstructured.SetNestedField(snapshot.Object, workspaceSnapshotClass, "spec", "volumeSnapshotClassName"))
	require.NoError(t, unstructured.SetNestedField(snapshot.Object, claim.Name, "spec", "source", "persistentVolumeClaimName"))
	createWorkspaceObject(t, kube, snapshot)
	var contentName string
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		current := &unstructured.Unstructured{}
		current.SetGroupVersionKind(snapshotGVK)
		if !assert.NoError(c, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(snapshot), current)) {
			return
		}
		ready, _, _ := unstructured.NestedBool(current.Object, "status", "readyToUse")
		contentName, _, _ = unstructured.NestedString(current.Object, "status", "boundVolumeSnapshotContentName")
		assert.True(c, ready, "VolumeSnapshot %s readyToUse, status %v", snapshot.GetName(), current.Object["status"])
		assert.NotEmpty(c, contentName)
	}, 3*time.Minute, time.Second)
	content := &unstructured.Unstructured{}
	content.SetGroupVersionKind(snapshotContent)
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Name: contentName}, content))
	driver, _, err := unstructured.NestedString(content.Object, "spec", "driver")
	require.NoError(t, err)
	handle, _, err := unstructured.NestedString(content.Object, "status", "snapshotHandle")
	require.NoError(t, err)
	require.NotEmpty(t, driver)
	require.NotEmpty(t, handle)
	return snapshot.GetName(), grant.Snapshot{Driver: driver, Handle: handle}
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

func TestWorkspaceSnapshotFixture(t *testing.T) {
	t.Parallel()
	interactionTarget(t)
	fixture := newWorkspaceFixture(t)
	require.Equal(t, workspaceSnapshotDriver, fixture.Snapshot.Driver)
	t.Logf("VolumeSnapshot %s: driver %s, handle %s, heads %v", fixture.SnapshotName, fixture.Snapshot.Driver, fixture.Snapshot.Handle, fixture.Heads)

	// The snapshot holds the clones: a volume restored from it reads the same
	// heads, and no clone kept the origin's credential.
	kube := interactionKubeClient(t)
	restored := createWorkspaceVolume(t, kube, &fixture.SnapshotName)
	report := runWorkspaceJob(t, kube, restored, workspaceHeadsScript+
		`! grep -rlF -e "$GIT_TOKEN" /workspace >&2`, []corev1.EnvVar{{Name: "GIT_TOKEN", Value: workspaceGitToken}})
	heads := parseWorkspaceHeads(t, report)
	for _, repository := range workspaceRepositories {
		require.Equal(t, workspaceCheckout{branch: "main", commit: fixture.Heads[repository]}, heads[repository], "restored %s", repository)
	}

	// The origin moved on the changed repository only.
	for _, repository := range workspaceRepositories {
		moved := fixture.Origin.head(t, repository+".git") != fixture.Heads[repository]
		require.Equal(t, repository == workspaceChanged, moved, "origin of %s moved after the snapshot", repository)
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
