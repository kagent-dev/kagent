package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// An answer is replayed into a native conversation, so its shape has to be the
// one the agent asked for. Both harnesses apply these rules, which is the point
// of them living here: the same answer used to be refused by one and accepted
// by the other.
func TestValidateAskUserAnswers(t *testing.T) {
	request := &runtime.AskUserRequest{ID: "request", Questions: []runtime.AskUserQuestion{
		{ID: "one", Question: "Which database?", Choices: []string{"PostgreSQL", "MySQL"}},
		{ID: "two", Question: "Which regions?", Choices: []string{"us-east-1", "eu-west-1"}, Multiple: true},
	}}
	for _, test := range []struct {
		name    string
		answers [][]string
		id      string
		wantErr string
	}{
		{name: "one answer each", answers: [][]string{{"PostgreSQL"}, {"us-east-1"}}},
		{name: "several where several were offered", answers: [][]string{{"PostgreSQL"}, {"us-east-1", "eu-west-1"}}},
		{name: "no answer at all", answers: [][]string{{}, {"us-east-1"}}, wantErr: "question 1 has no answer"},
		{name: "two answers to a single choice", answers: [][]string{{"PostgreSQL", "MySQL"}, {"us-east-1"}}, wantErr: "question 1 takes one answer"},
		{name: "a question left out", answers: [][]string{{"PostgreSQL"}}, wantErr: "every pending question"},
		{name: "an answer too many", answers: [][]string{{"PostgreSQL"}, {"us-east-1"}, {"extra"}}, wantErr: "every pending question"},
		{name: "a response to another request", answers: [][]string{{"PostgreSQL"}, {"us-east-1"}}, id: "other", wantErr: "does not match pending ID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := request.ID
			if test.id != "" {
				id = test.id
			}
			err := runtime.ValidateAskUserAnswers(request, &runtime.AskUserResponse{ID: id, Answers: test.answers})
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
	require.Error(t, runtime.ValidateAskUserAnswers(nil, &runtime.AskUserResponse{}))
	require.Error(t, runtime.ValidateAskUserAnswers(request, nil))
}
