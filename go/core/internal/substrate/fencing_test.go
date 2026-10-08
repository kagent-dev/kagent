package substrate

import (
	"context"
	"errors"
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// errTokenRefused is ate-api's refusal of a request carrying a field it has no
// descriptor for, as a Substrate without the fencing token returns it.
var errTokenRefused = status.Error(codes.InvalidArgument, "request: Invalid value: unknown field with protobuf tag 10001")

// fencingFake records the token of every Pause, Suspend and Resume and refuses
// any token while takesToken is false.
type fencingFake struct {
	ateapipb.ControlClient
	takesToken bool
	tokens     []*ateapipb.FencingToken
}

func (f *fencingFake) answer(token *ateapipb.FencingToken) error {
	f.tokens = append(f.tokens, token)
	if token != nil && !f.takesToken {
		return errTokenRefused
	}
	return nil
}

func (f *fencingFake) PauseActor(_ context.Context, req *ateapipb.PauseActorRequest, _ ...grpc.CallOption) (*ateapipb.PauseActorResponse, error) {
	if err := f.answer(req.GetFencingToken()); err != nil {
		return nil, err
	}
	return &ateapipb.PauseActorResponse{Actor: &ateapipb.Actor{}}, nil
}

func (f *fencingFake) SuspendActor(_ context.Context, req *ateapipb.SuspendActorRequest, _ ...grpc.CallOption) (*ateapipb.SuspendActorResponse, error) {
	if err := f.answer(req.GetFencingToken()); err != nil {
		return nil, err
	}
	return &ateapipb.SuspendActorResponse{Actor: &ateapipb.Actor{}}, nil
}

func (f *fencingFake) ResumeActor(_ context.Context, req *ateapipb.ResumeActorRequest, _ ...grpc.CallOption) (*ateapipb.ResumeActorResponse, error) {
	if err := f.answer(req.GetFencingToken()); err != nil {
		return nil, err
	}
	return &ateapipb.ResumeActorResponse{Actor: &ateapipb.Actor{}}, nil
}

func TestFencedCalls(t *testing.T) {
	token := &ateapipb.FencingToken{Holder: "executor", Generation: 7}
	calls := map[string]func(*Client, context.Context) error{
		"pause": func(c *Client, ctx context.Context) error {
			_, err := c.PauseActor(ctx, "kagent", "session-a")
			return err
		},
		"suspend": func(c *Client, ctx context.Context) error {
			_, err := c.SuspendActor(ctx, "kagent", "session-a")
			return err
		},
		"resume": func(c *Client, ctx context.Context) error {
			_, err := c.ResumeActor(ctx, "kagent", "session-a")
			return err
		},
	}
	for name, call := range calls {
		t.Run(name+" carries the token to a Substrate that takes it", func(t *testing.T) {
			fake := &fencingFake{takesToken: true}
			c := &Client{ControlClient: fake}

			require.NoError(t, call(c, WithFencingToken(context.Background(), token)))
			require.NoError(t, call(c, WithFencingToken(context.Background(), token)))
			require.Equal(t, []*ateapipb.FencingToken{token, token}, fake.tokens)
			require.False(t, c.fencingUnsupported.Load())
		})

		t.Run(name+" falls back to an unfenced call once Substrate refuses the token", func(t *testing.T) {
			fake := &fencingFake{}
			c := &Client{ControlClient: fake}

			require.NoError(t, call(c, WithFencingToken(context.Background(), token)))
			require.Equal(t, []*ateapipb.FencingToken{token, nil}, fake.tokens)
			require.True(t, c.fencingUnsupported.Load())

			// Remembered: the next call is sent unfenced at once.
			require.NoError(t, call(c, WithFencingToken(context.Background(), token)))
			require.Equal(t, []*ateapipb.FencingToken{token, nil, nil}, fake.tokens)
		})

		t.Run(name+" without a token is sent unfenced", func(t *testing.T) {
			fake := &fencingFake{}
			c := &Client{ControlClient: fake}

			require.NoError(t, call(c, context.Background()))
			require.Equal(t, []*ateapipb.FencingToken{nil}, fake.tokens)
			require.False(t, c.fencingUnsupported.Load())
		})
	}
}

func TestFencedKeepsOtherErrors(t *testing.T) {
	token := &ateapipb.FencingToken{Holder: "executor", Generation: 7}
	for name, refusal := range map[string]error{
		"a newer holder's token": status.Error(codes.FailedPrecondition, "Actor kagent/session-a is fenced by a newer token"),
		"another invalid field":  status.Error(codes.InvalidArgument, "request: Invalid value: unknown field with protobuf tag 9999"),
		"a transport error":      errors.New("connection reset"),
	} {
		t.Run(name, func(t *testing.T) {
			c := &Client{}
			var sent []*ateapipb.FencingToken
			_, err := fenced(WithFencingToken(context.Background(), token), c, func(token *ateapipb.FencingToken) (*ateapipb.Actor, error) {
				sent = append(sent, token)
				return nil, refusal
			})
			require.ErrorIs(t, err, refusal)
			require.Equal(t, []*ateapipb.FencingToken{token}, sent)
			require.False(t, c.fencingUnsupported.Load())
		})
	}
}
