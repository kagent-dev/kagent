package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// staticSessionWorkflow is the session workflow of the tests that drive turns
// through a gateway against a fake runtime: no Substrate stands behind the
// sessions, so a lifecycle call leaves every session as it is, no runtime is
// lost and nothing moves to another revision before a turn.
type staticSessionWorkflow struct{}

func (staticSessionWorkflow) Create(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return session, nil
}

func (staticSessionWorkflow) Suspend(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return session, nil
}

func (staticSessionWorkflow) Resume(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return session, nil
}

func (staticSessionWorkflow) Delete(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return session, nil
}

func (staticSessionWorkflow) RuntimeLost(context.Context, *apiv1alpha1.Session) (string, bool, error) {
	return "", false, nil
}

func (staticSessionWorkflow) RepointQuiesced(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return session, nil
}
