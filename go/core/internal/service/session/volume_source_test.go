package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// recordingAdmission answers every request with err and records what it was
// asked, so a test can see what reaches the installation's admission.
type recordingAdmission struct {
	asked    bool
	caller   string
	source   *apiv1alpha1.SessionVolumeSource
	token    string
	admitted Admitted
	err      error
}

func (a *recordingAdmission) Admit(_ context.Context, caller string, source *apiv1alpha1.SessionVolumeSource, token string) (Admitted, error) {
	a.asked, a.caller, a.source, a.token = true, caller, source, token
	if a.err != nil {
		return Admitted{}, a.err
	}
	return a.admitted, nil
}

// testVolumeSource is a workspace volume with the session's own directory
// read-write and the workspace's mirrors read-only.
func testVolumeSource() *apiv1alpha1.SessionVolumeSource {
	return &apiv1alpha1.SessionVolumeSource{
		Volume: &apiv1alpha1.SessionVolume{CsiDriver: "efs.csi.aws.com", VolumeHandle: "fs-0123456789abcdef0::fsap-0123456789abcdef0"},
		Mounts: []*apiv1alpha1.SessionVolumeMount{
			{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace"},
			{SubPath: "mirrors", MountPath: "/mirrors", ReadOnly: true},
		},
	}
}

func testCreateRequest() CreateRequest {
	return CreateRequest{Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}, RequestID: "request-1"}
}

func TestServiceCreateRefusesAVolumeSourceWithoutAnAdmission(t *testing.T) {
	for _, test := range []struct {
		name   string
		source *apiv1alpha1.SessionVolumeSource
		token  string
	}{
		{name: "volume source", source: testVolumeSource()},
		{name: "admission token", token: "grant"},
		{name: "both", source: testVolumeSource(), token: "grant"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceTestStore{}
			service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
			request := testCreateRequest()
			request.VolumeSource, request.AdmissionToken = test.source, test.token
			_, err := service.Create(serviceTestContext("alice"), request)
			require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition), "Create() error = %v", err)
			require.ErrorContains(t, err, "this installation admits no Session volume")
			require.Nil(t, store.createInput, "a refused request must not reserve a Session")
		})
	}
}

func TestServiceCreateAdmitsAVolumeSourceThroughTheAdmission(t *testing.T) {
	provider := &apiv1alpha1.SessionProvider{Hostname: "github.com", Audience: "https://github.com", Scheme: "Basic", Username: "x-access-token", Broker: "workspaces"}
	admission := &recordingAdmission{admitted: Admitted{Providers: []*apiv1alpha1.SessionProvider{provider}, Changed: []string{"acme/api"}}}
	store := &serviceTestStore{}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{}, WithVolumeAdmission(admission))
	source := testVolumeSource()
	request := testCreateRequest()
	request.VolumeSource, request.AdmissionToken = source, "admission-token-never-stored"

	session, err := service.Create(serviceTestContext("alice"), request)
	require.NoError(t, err)
	require.Equal(t, "alice", admission.caller, "the admission sees the authenticated caller")
	require.True(t, proto.Equal(source, admission.source), "the admission sees the request's source")
	require.Equal(t, "admission-token-never-stored", admission.token)
	require.True(t, proto.Equal(source, store.createInput.GetVolumeSource()), "the reserved Session carries the source")
	require.True(t, proto.Equal(source, session.GetVolumeSource()), "the response carries the source")
	for name, admitted := range map[string]*apiv1alpha1.Session{"reserved": store.createInput, "response": session} {
		require.Len(t, admitted.GetProviders(), 1, "the %s Session carries the admitted providers", name)
		require.True(t, proto.Equal(provider, admitted.GetProviders()[0]), "the %s Session's provider", name)
		require.Equal(t, []string{"acme/api"}, admitted.GetChanged(), "the %s Session carries the admitted changed list", name)
	}
	stored, err := proto.Marshal(store.createInput)
	require.NoError(t, err)
	require.NotContains(t, string(stored), "admission-token-never-stored", "the token reaches the store nowhere")
}

func TestServiceCreateMapsAnAdmissionRefusal(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code serviceerrors.Code
	}{
		{name: "the admission's own answer stands", err: serviceerrors.NewFailedPrecondition("grant expired", nil), code: serviceerrors.CodeFailedPrecondition},
		{name: "any other failure denies", err: errors.New("signature invalid"), code: serviceerrors.CodePermissionDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceTestStore{}
			service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{}, WithVolumeAdmission(&recordingAdmission{err: test.err}))
			request := testCreateRequest()
			request.VolumeSource, request.AdmissionToken = testVolumeSource(), "grant"
			_, err := service.Create(serviceTestContext("alice"), request)
			require.True(t, serviceerrors.IsCode(err, test.code), "Create() error = %v, want code %s", err, test.code)
			require.Nil(t, store.createInput, "a refused request must not reserve a Session")
		})
	}
}

// A request without the volume fields never consults the admission and reserves
// a Session without a source, with or without an admission configured.
func TestServiceCreateWithoutTheVolumeFieldsNeverAsksTheAdmission(t *testing.T) {
	for _, test := range []struct {
		name    string
		options []Option
	}{
		{name: "no admission"},
		{name: "an admission that refuses everything", options: []Option{WithVolumeAdmission(&recordingAdmission{err: errors.New("must not be asked")})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceTestStore{}
			service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{}, test.options...)
			session, err := service.Create(serviceTestContext("alice"), testCreateRequest())
			require.NoError(t, err)
			require.Nil(t, store.createInput.GetVolumeSource())
			require.Nil(t, session.GetVolumeSource())
		})
	}
}

// Validation of the source precedes the admission: a malformed source is
// InvalidArgument whatever the installation configures, and never reaches it.
func TestServiceCreateRejectsAnInvalidVolumeSource(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*apiv1alpha1.SessionVolumeSource)
		want   string
	}{
		{name: "no volume", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Volume = nil }, want: "csi_driver"},
		{name: "driver with uppercase", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Volume.CsiDriver = "EFS.csi.aws.com" }, want: "csi_driver"},
		{name: "driver too long", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Volume.CsiDriver = strings.Repeat("a", 64) }, want: "csi_driver"},
		{name: "empty handle", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Volume.VolumeHandle = "" }, want: "volume_handle"},
		{name: "handle with whitespace", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Volume.VolumeHandle = "fs 01" }, want: "volume_handle"},
		{name: "handle with a control character", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Volume.VolumeHandle = "fs\x0001" }, want: "volume_handle"},
		{name: "handle too long", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Volume.VolumeHandle = strings.Repeat("a", 1025) }, want: "volume_handle"},
		{name: "no mounts", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts = nil }, want: "mounts"},
		{name: "too many mounts", mutate: func(s *apiv1alpha1.SessionVolumeSource) {
			for i := range 8 {
				s.Mounts = append(s.Mounts, &apiv1alpha1.SessionVolumeMount{SubPath: "mirrors", MountPath: "/m" + strings.Repeat("x", i+1), ReadOnly: true})
			}
		}, want: "mounts"},
		{name: "absolute sub-path", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].SubPath = "/sessions" }, want: "sub_path"},
		{name: "empty sub-path", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].SubPath = "" }, want: "sub_path"},
		{name: "sub-path leaving the volume", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].SubPath = "sessions/../mirrors" }, want: "sub_path"},
		{name: "sub-path with a dot segment", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].SubPath = "./sessions" }, want: "sub_path"},
		{name: "sub-path with an empty segment", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].SubPath = "sessions//a" }, want: "sub_path"},
		{name: "sub-path with whitespace", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].SubPath = "sessions/a b" }, want: "sub_path"},
		{name: "relative mount path", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].MountPath = "workspace" }, want: "mount_path"},
		{name: "root mount path", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].MountPath = "/" }, want: "mount_path"},
		{name: "unclean mount path", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].MountPath = "/workspace/" }, want: "mount_path"},
		{name: "mount path with a parent segment", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].MountPath = "/workspace/../etc" }, want: "mount_path"},
		{name: "mount at the durable directory", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].MountPath = "/data" }, want: "outside /data"},
		{name: "mount under the durable directory", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].MountPath = "/data/workspace" }, want: "outside /data"},
		{name: "two read-write mounts", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[1].ReadOnly = false }, want: "one read-write mount"},
		{name: "two mounts at one path", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[1].MountPath = "/workspace" }, want: "unique"},
		// The actor mounts the volume where every revision's ActorTemplate
		// declares it: the session's own directory read-write at /workspace and
		// the shared directory read-only at /mirrors.
		{name: "the mirrors alone", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts = s.Mounts[1:] }, want: "read-write at /workspace"},
		{name: "the own directory elsewhere", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].MountPath = "/home/agent" }, want: "workspace layout"},
		{name: "a third mount", mutate: func(s *apiv1alpha1.SessionVolumeSource) {
			s.Mounts = append(s.Mounts, &apiv1alpha1.SessionVolumeMount{SubPath: "cache", MountPath: "/cache", ReadOnly: true})
		}, want: "workspace layout"},
		{name: "the own directory read-only", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].ReadOnly = true }, want: "workspace layout"},
		{name: "a handle longer than Substrate takes", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Volume.VolumeHandle = strings.Repeat("h", 257) }, want: "at most 256"},
	} {
		t.Run(test.name, func(t *testing.T) {
			admission := &recordingAdmission{}
			store := &serviceTestStore{}
			service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{}, WithVolumeAdmission(admission))
			request := testCreateRequest()
			request.VolumeSource, request.AdmissionToken = testVolumeSource(), "grant"
			test.mutate(request.VolumeSource)
			_, err := service.Create(serviceTestContext("alice"), request)
			require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), "Create() error = %v", err)
			require.ErrorContains(t, err, test.want)
			require.False(t, admission.asked, "an invalid source must not reach the admission")
			require.Nil(t, store.createInput)
		})
	}
	for _, test := range []struct {
		name   string
		mutate func(*apiv1alpha1.SessionVolumeSource)
	}{
		{name: "the session's own directory alone", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts = s.Mounts[:1] }},
		{name: "a hidden directory", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[1].SubPath = ".mirrors" }},
		{name: "the session's id alone as the directory", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Mounts[0].SubPath = "${SESSION_ID}" }},
		{name: "a handle of Substrate's longest", mutate: func(s *apiv1alpha1.SessionVolumeSource) { s.Volume.VolumeHandle = strings.Repeat("h", 256) }},
	} {
		t.Run("accepts "+test.name, func(t *testing.T) {
			admission := &recordingAdmission{}
			store := &serviceTestStore{}
			service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{}, WithVolumeAdmission(admission))
			request := testCreateRequest()
			request.VolumeSource = testVolumeSource()
			test.mutate(request.VolumeSource)
			session, err := service.Create(serviceTestContext("alice"), request)
			require.NoError(t, err)
			require.True(t, proto.Equal(request.VolumeSource, session.GetVolumeSource()))
		})
	}
}
