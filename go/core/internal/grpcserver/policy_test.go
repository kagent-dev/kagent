package grpcserver

import (
	"testing"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestAgentInstanceServicePoliciesMatchTheirEffect pins the access mode of every
// method on the service. A wrong mode here fails silently in one of two ways: a
// write classified as a read is reachable through a read-only share link, and a
// read classified as a write is refused for a legitimate caller. Neither shows
// up as an error anywhere near the policy table.
func TestAgentInstanceServicePoliciesMatchTheirEffect(t *testing.T) {
	policies := DefaultMethodPolicies()
	for _, test := range []struct {
		name   string
		method string
		want   AccessMode
	}{
		{name: "create", method: apiv1alpha1.AgentInstanceService_CreateAgentInstance_FullMethodName, want: AccessCreate},
		{name: "get", method: apiv1alpha1.AgentInstanceService_GetAgentInstance_FullMethodName, want: AccessRead},
		{name: "list", method: apiv1alpha1.AgentInstanceService_ListAgentInstances_FullMethodName, want: AccessRead},
		{name: "rename is a write", method: apiv1alpha1.AgentInstanceService_UpdateAgentInstanceName_FullMethodName, want: AccessUpdate},
		{name: "suspend", method: apiv1alpha1.AgentInstanceService_SuspendAgentInstance_FullMethodName, want: AccessUpdate},
		{name: "resume", method: apiv1alpha1.AgentInstanceService_ResumeAgentInstance_FullMethodName, want: AccessUpdate},
		{name: "delete", method: apiv1alpha1.AgentInstanceService_DeleteAgentInstance_FullMethodName, want: AccessDelete},
		{name: "create share", method: apiv1alpha1.AgentInstanceService_CreateAgentInstanceShare_FullMethodName, want: AccessCreate},
		{name: "list shares", method: apiv1alpha1.AgentInstanceService_ListAgentInstanceShares_FullMethodName, want: AccessRead},
		{name: "revoke share", method: apiv1alpha1.AgentInstanceService_RevokeAgentInstanceShare_FullMethodName, want: AccessDelete},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := policies[test.method]
			if !ok {
				t.Fatalf("%s has no policy; an unconfigured method is denied outright", test.method)
			}
			if got != test.want {
				t.Fatalf("%s policy = %q, want %q", test.method, got, test.want)
			}
		})
	}
}

// TestReadOnlyShareCannotRenameAConversation is the property the policy entry
// exists for, measured through the interceptor rather than read off the table: a
// read-only share link may open a conversation and must not be able to retitle
// it for everyone who holds the link.
func TestReadOnlyShareCannotRenameAConversation(t *testing.T) {
	session := &testSession{principal: pkgauth.Principal{User: pkgauth.User{ID: "visitor"}}}
	for _, test := range []struct {
		name       string
		permission string
		method     string
		wantCode   codes.Code
	}{
		{
			name: "read-only share may read", permission: "READ_ONLY",
			method: apiv1alpha1.AgentInstanceService_GetAgentInstance_FullMethodName, wantCode: codes.OK,
		},
		{
			name: "read-only share may not rename", permission: "READ_ONLY",
			method: apiv1alpha1.AgentInstanceService_UpdateAgentInstanceName_FullMethodName, wantCode: codes.PermissionDenied,
		},
		{
			name: "read-write share may rename", permission: "READ_WRITE",
			method: apiv1alpha1.AgentInstanceService_UpdateAgentInstanceName_FullMethodName, wantCode: codes.OK,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			shareStore := &testShareStore{
				instanceShare: &dbpkg.AgentInstanceShare{
					ID: "share-1", InstanceID: "instance-1", Permission: test.permission, OwnerUserID: "owner",
				},
			}
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-share-token", "token"))
			_, err := authenticate(ctx, test.method, &testAuthenticator{session: session}, shareStore, DefaultMethodPolicies())
			if got := status.Code(err); got != test.wantCode {
				t.Fatalf("authenticate(%s) code = %v (%v), want %v", test.method, got, err, test.wantCode)
			}
		})
	}
}
