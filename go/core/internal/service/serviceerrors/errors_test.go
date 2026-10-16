package serviceerrors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestNewNotAuthorized(t *testing.T) {
	for _, test := range []struct {
		name  string
		cause error
		want  Code
	}{
		{name: "a refusal", cause: errors.New("alice may not get agents"), want: CodePermissionDenied},
		{name: "no cause", want: CodePermissionDenied},
		{name: "an answer the API server did not give", cause: fmt.Errorf("%w: the API server is unreachable", auth.ErrReviewUnavailable), want: CodeUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := NewNotAuthorized("Not authorized", test.cause)
			if err.Code() != test.want {
				t.Fatalf("code = %s, want %s", err.Code(), test.want)
			}
			if !errors.Is(err, test.cause) && test.cause != nil {
				t.Fatalf("cause %v is not wrapped", test.cause)
			}
		})
	}
}
