package client

import (
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestExecutionContractValidation(t *testing.T) {
	validator, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "11111111-1111-4111-8111-111111111111"
	for _, test := range []struct {
		name    string
		message proto.Message
		valid   bool
	}{
		{"session authority", &apiv1.GetSessionAuthorityRequest{SessionId: sessionID}, true},
		{"invalid session", &apiv1.GetSessionAuthorityRequest{SessionId: "invalid"}, false},
		{"revocation", &apiv1.RevokeSessionAuthorityRequest{SessionId: sessionID}, true},
		{"task", &apiv1.GetTaskExecutionRequest{TaskId: "task-1"}, true},
		{"missing task", &apiv1.GetTaskExecutionRequest{}, false},
		{"task reference", &apiv1.TaskReference{SessionId: sessionID, TaskId: "task-1"}, true},
		{"invalid task session", &apiv1.TaskReference{SessionId: "invalid", TaskId: "task-1"}, false},
		{"paged tree", &apiv1.ListTaskExecutionsRequest{RootTaskId: "root", Page: &apiv1.PageRequest{Limit: 100}}, true},
		{"oversized page", &apiv1.ListTaskExecutionsRequest{RootTaskId: "root", Page: &apiv1.PageRequest{Limit: 101}}, false},
		{"empty ceiling", &apiv1.ScopeCeiling{}, true},
		{"scopes", &apiv1.ScopeCeiling{Scopes: []string{"read", "write"}}, true},
		{"duplicate scopes", &apiv1.ScopeCeiling{Scopes: []string{"read", "read"}}, false},
		{"whitespace scope", &apiv1.ScopeCeiling{Scopes: []string{"read write"}}, false},
		{"refresh credential", &apiv1.RegisterDelegationCredentialRequest{RefreshToken: "refresh"}, true},
		{"missing credential", &apiv1.RegisterDelegationCredentialRequest{}, false},
		{"oversized credential", &apiv1.RegisterDelegationCredentialRequest{RefreshToken: strings.Repeat("x", 16385)}, false},
		{"bounded credential", &apiv1.IssueSessionCredentialRequest{SessionId: sessionID, Ttl: durationpb.New(time.Hour)}, true},
		{"missing ttl", &apiv1.IssueSessionCredentialRequest{SessionId: sessionID}, false},
		{"zero ttl", &apiv1.IssueSessionCredentialRequest{SessionId: sessionID, Ttl: durationpb.New(0)}, false},
		{"excessive ttl", &apiv1.IssueSessionCredentialRequest{SessionId: sessionID, Ttl: durationpb.New(time.Hour + time.Second)}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validator.Validate(test.message); (err == nil) != test.valid {
				t.Fatalf("Validate() = %v, valid = %t", err, test.valid)
			}
		})
	}
}
