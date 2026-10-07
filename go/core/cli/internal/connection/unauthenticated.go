package connection

import (
	"context"
	"fmt"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// The controller answers a bearer it cannot identify the caller by with a bare
// "invalid credentials". These interceptors add what the person can do about
// it, which depends on the token this CLI sent, or did not send.

func (o *Options) unaryInterceptor(ctx context.Context, method string, request, reply any, connection *grpc.ClientConn, invoker grpc.UnaryInvoker, callOptions ...grpc.CallOption) error {
	return o.explainUnauthenticated(ctx, invoker(ctx, method, request, reply, connection, callOptions...))
}

func (o *Options) streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, connection *grpc.ClientConn, method string, streamer grpc.Streamer, callOptions ...grpc.CallOption) (grpc.ClientStream, error) {
	stream, err := streamer(ctx, desc, connection, method, callOptions...)
	if err != nil {
		return nil, o.explainUnauthenticated(ctx, err)
	}
	return &explainedStream{ClientStream: stream, ctx: ctx, options: o}, nil
}

// explainedStream explains the refusal where a stream reports it: on receive.
type explainedStream struct {
	grpc.ClientStream
	ctx     context.Context
	options *Options
}

func (s *explainedStream) RecvMsg(message any) error {
	return s.options.explainUnauthenticated(s.ctx, s.ClientStream.RecvMsg(message))
}

func (o *Options) explainUnauthenticated(ctx context.Context, err error) error {
	if status.Code(err) != codes.Unauthenticated {
		return err
	}
	return status.Error(codes.Unauthenticated, status.Convert(err).Message()+": "+o.explainCallerToken(ctx))
}

func (o *Options) explainCallerToken(ctx context.Context) string {
	if !o.token.isSet() {
		return "no caller token was sent; " + callerTokenAdvice
	}
	outgoing, _ := metadata.FromOutgoingContext(ctx)
	if !slices.Contains(outgoing.Get(authorizationMetadataKey), bearerPrefix+o.token.value) {
		return fmt.Sprintf("the call carried the model key of --token as its bearer in place of the caller token from %s; a controller that identifies the caller by the bearer takes no model key there, so drop --token", o.token.source)
	}
	return fmt.Sprintf("the caller token from %s was refused", o.token.source)
}
