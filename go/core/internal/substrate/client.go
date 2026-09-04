package substrate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
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

	tlsConfig, err := ateAPITLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	opts := []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))}

	conn, err := grpc.NewClient(cfg.AteAPIEndpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("substrate: dial ate-api %q: %w", cfg.AteAPIEndpoint, err)
	}
	// NewClient stays idle until Connect() or an RPC; waitConnReady enforces DialTimeout.
	conn.Connect()
	if err := waitConnReady(dialCtx, conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("substrate: dial ate-api %q: %w", cfg.AteAPIEndpoint, err)
	}

	return &Client{
		ControlClient: ateapipb.NewControlClient(conn),
		conn:          conn,
		cfg:           cfg,
	}, nil
}

func ateAPITLSConfig(cfg Config) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("substrate: read ate-api CA file: %w", err)
		}
		tlsCfg.RootCAs = x509.NewCertPool()
		if !tlsCfg.RootCAs.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("substrate: ate-api CA file %q contains no certificates", cfg.CAFile)
		}
	}
	if cfg.ClientCertFile != "" {
		tlsCfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientCertFile)
			if err != nil {
				return nil, fmt.Errorf("substrate: load ate-api client certificate: %w", err)
			}
			return &cert, nil
		}
	}
	return tlsCfg, nil
}

func waitConnReady(ctx context.Context, conn *grpc.ClientConn) error {
	for {
		switch s := conn.GetState(); s {
		case connectivity.Ready:
			return nil
		case connectivity.Shutdown:
			return fmt.Errorf("connection shut down")
		default:
			if !conn.WaitForStateChange(ctx, s) {
				if err := ctx.Err(); err != nil {
					return err
				}
				return fmt.Errorf("connection closed before ready")
			}
		}
	}
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

// actorRef builds the (atespace, name) reference used by Get/Resume/Suspend/Delete.
// v0.0.9 renamed the ActorRef message to ObjectRef.
func actorRef(atespace, actorID string) *ateapipb.ObjectRef {
	return &ateapipb.ObjectRef{Atespace: atespace, Name: actorID}
}

func (c *Client) GetActor(ctx context.Context, atespace, actorID string) (*ateapipb.Actor, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.GetActor(ctx, &ateapipb.GetActorRequest{Actor: actorRef(atespace, actorID)})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetActorTemplate(ctx context.Context, atespace, name string) (*ateapipb.ActorTemplate, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.ControlClient.GetActorTemplate(ctx, &ateapipb.GetActorTemplateRequest{ActorTemplate: actorRef(atespace, name)})
}

func (c *Client) CreateActorTemplate(ctx context.Context, template *ateapipb.ActorTemplate) (*ateapipb.ActorTemplate, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.ControlClient.CreateActorTemplate(ctx, &ateapipb.CreateActorTemplateRequest{ActorTemplate: template})
}

// DeleteActorTemplate also removes the template's golden Actor. Substrate
// documents that behavior but does not implement it yet.
func (c *Client) DeleteActorTemplate(ctx context.Context, atespace, name, uid string) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err := c.ControlClient.DeleteActorTemplate(ctx, &ateapipb.DeleteActorTemplateRequest{ActorTemplate: actorRef(atespace, name)})
	if err != nil && status.Code(err) != codes.NotFound {
		return err
	}
	// ponytail: this two-RPC cleanup cannot share Substrate's template lease;
	// remove it when DeleteActorTemplate fulfills its golden-Actor contract.
	_, err = c.ControlClient.DeleteActor(ctx, &ateapipb.DeleteActorRequest{
		Actor:    actorRef("ate-golden", uid),
		AnyState: true,
	})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return err
}

func (c *Client) CreateActor(ctx context.Context, atespace, actorID, tmplNS, tmplName string) (*ateapipb.Actor, error) {
	return c.createActor(ctx, atespace, actorID, tmplNS, tmplName, nil)
}

func (c *Client) CreateActorFromSnapshotTag(ctx context.Context, atespace, actorID, tmplNS, tmplName, tagAtespace, tagName string) (*ateapipb.Actor, error) {
	return c.createActor(ctx, atespace, actorID, tmplNS, tmplName, &ateapipb.ObjectRef{Atespace: tagAtespace, Name: tagName})
}

func (c *Client) createActor(ctx context.Context, atespace, actorID, tmplNS, tmplName string, source *ateapipb.ObjectRef) (*ateapipb.Actor, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.CreateActor(ctx, &ateapipb.CreateActorRequest{
		Actor: &ateapipb.Actor{
			Metadata:          &ateapipb.ResourceMetadata{Atespace: atespace, Name: actorID},
			ActorTemplate:     actorRef(tmplNS, tmplName),
			SourceSnapshotTag: source,
		},
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) ResumeActor(ctx context.Context, atespace, actorID string) (*ateapipb.Actor, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.ResumeActor(ctx, &ateapipb.ResumeActorRequest{Actor: actorRef(atespace, actorID)})
	if err != nil {
		return nil, err
	}
	return resp.GetActor(), nil
}

func (c *Client) PauseActor(ctx context.Context, atespace, actorID string) (*ateapipb.Actor, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.PauseActor(ctx, &ateapipb.PauseActorRequest{Actor: actorRef(atespace, actorID)})
	if err != nil {
		return nil, err
	}
	return resp.GetActor(), nil
}

func (c *Client) SuspendActor(ctx context.Context, atespace, actorID string) (*ateapipb.Actor, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.ControlClient.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: actorRef(atespace, actorID)})
	if err != nil {
		return nil, err
	}
	return resp.GetActor(), nil
}

func (c *Client) GetActorSnapshot(ctx context.Context, atespace, name string) (*ateapipb.ActorSnapshot, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.ControlClient.GetActorSnapshot(ctx, &ateapipb.GetActorSnapshotRequest{ActorSnapshot: actorRef(atespace, name)})
}

func (c *Client) GetActorSnapshotTag(ctx context.Context, atespace, name string) (*ateapipb.ActorSnapshotTag, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.ControlClient.GetActorSnapshotTag(ctx, &ateapipb.GetActorSnapshotTagRequest{ActorSnapshotTag: actorRef(atespace, name)})
}

func (c *Client) CreateActorSnapshotTag(ctx context.Context, atespace, name, snapshotName string) (*ateapipb.ActorSnapshotTag, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	return c.ControlClient.CreateActorSnapshotTag(ctx, &ateapipb.CreateActorSnapshotTagRequest{
		ActorSnapshotTag: &ateapipb.ActorSnapshotTag{
			Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: name},
			Snapshot: actorRef(atespace, snapshotName),
			Scope:    ateapipb.ActorSnapshotTagScope_ACTOR_SNAPSHOT_TAG_SCOPE_ATESPACE,
		},
	})
}

func (c *Client) DeleteActorSnapshotTag(ctx context.Context, atespace, name string) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err := c.ControlClient.DeleteActorSnapshotTag(ctx, &ateapipb.DeleteActorSnapshotTagRequest{ActorSnapshotTag: actorRef(atespace, name)})
	return err
}

func (c *Client) DeleteActor(ctx context.Context, atespace, actorID string) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err := c.ControlClient.DeleteActor(ctx, &ateapipb.DeleteActorRequest{Actor: actorRef(atespace, actorID)})
	return err
}

// EnsureAtespace idempotently ensures the named atespace exists on the substrate side.
// Actors cannot be created into a nonexistent atespace (FailedPrecondition).
func (c *Client) EnsureAtespace(ctx context.Context, name string) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err := c.CreateAtespace(ctx, &ateapipb.CreateAtespaceRequest{
		Atespace: &ateapipb.Atespace{Metadata: &ateapipb.ResourceMetadata{Name: name}},
	})
	if err != nil && status.Code(err) == codes.AlreadyExists {
		return nil
	}
	return err
}
