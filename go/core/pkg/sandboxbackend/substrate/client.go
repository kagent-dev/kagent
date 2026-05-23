package substrate

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/agent-substrate/substrate/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps ate-api Control gRPC.
type Client struct {
	ateapipb.ControlClient
	conn *grpc.ClientConn
	cfg  Config
}

// Dial connects to the ate-api server.
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.AteAPIEndpoint == "" {
		return nil, fmt.Errorf("substrate: ate-api endpoint is required")
	}
	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	var opts []grpc.DialOption
	if cfg.Insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	}

	conn, err := grpc.NewClient(cfg.AteAPIEndpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("substrate: dial ate-api %q: %w", cfg.AteAPIEndpoint, err)
	}
	_ = dialCtx

	return &Client{
		ControlClient: ateapipb.NewControlClient(conn),
		conn:          conn,
		cfg:           cfg,
	}, nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.cfg.CallTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.cfg.CallTimeout)
}

func (c *Client) GetActor(ctx context.Context, actorID string) (*ateapipb.Actor, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.GetActor(ctx, &ateapipb.GetActorRequest{ActorId: actorID})
	if err != nil {
		return nil, err
	}
	return resp.GetActor(), nil
}

func (c *Client) CreateActor(ctx context.Context, actorID, tmplNS, tmplName string) (*ateapipb.Actor, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.CreateActor(ctx, &ateapipb.CreateActorRequest{
		ActorId:                actorID,
		ActorTemplateNamespace: tmplNS,
		ActorTemplateName:      tmplName,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetActor(), nil
}

func (c *Client) ResumeActor(ctx context.Context, actorID string) (*ateapipb.Actor, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.ResumeActor(ctx, &ateapipb.ResumeActorRequest{ActorId: actorID})
	if err != nil {
		return nil, err
	}
	return resp.GetActor(), nil
}

func (c *Client) SuspendActor(ctx context.Context, actorID string) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err := c.ControlClient.SuspendActor(ctx, &ateapipb.SuspendActorRequest{ActorId: actorID})
	return err
}

func (c *Client) DeleteActor(ctx context.Context, actorID string) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err := c.ControlClient.DeleteActor(ctx, &ateapipb.DeleteActorRequest{ActorId: actorID})
	return err
}
