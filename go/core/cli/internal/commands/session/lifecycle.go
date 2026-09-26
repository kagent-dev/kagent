package session

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type lifecycleClient interface {
	CreateSession(context.Context, *apiv1alpha1.CreateSessionRequest) (*apiv1alpha1.CreateSessionResponse, error)
	DeleteSession(context.Context, *apiv1alpha1.DeleteSessionRequest) (*apiv1alpha1.DeleteSessionResponse, error)
}

// CreateCfg configures Session creation.
type CreateCfg struct {
	OutputFormat string
	Agent        string
	RequestID    string
}

// DeleteCfg configures Session deletion.
type DeleteCfg struct {
	OutputFormat string
	SessionID    string
}

// runCreate creates a Session.
func runCreate(
	ctx context.Context,
	options connection.Options,
	cfg *CreateCfg,
	out io.Writer,
) (err error) {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	ensureRequestID(cfg)

	session, err := connection.OpenAPI(ctx, options)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, session.Close())
	}()
	return create(ctx, session.API.Session, session.Namespace, cfg, format, out)
}

func ensureRequestID(cfg *CreateCfg) {
	if cfg.RequestID == "" {
		cfg.RequestID = uuid.NewString()
	}
}

// runDelete deletes a Session.
func runDelete(
	ctx context.Context,
	options connection.Options,
	cfg *DeleteCfg,
	out io.Writer,
) (err error) {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	session, err := connection.OpenAPI(ctx, options)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, session.Close())
	}()
	return deleteSession(ctx, session.API.Session, cfg, format, out)
}

func create(
	ctx context.Context,
	client lifecycleClient,
	namespace string,
	cfg *CreateCfg,
	format clioutput.Format,
	out io.Writer,
) error {
	response, err := client.CreateSession(ctx, &apiv1alpha1.CreateSessionRequest{
		Agent: &apiv1alpha1.ResourceReference{Namespace: namespace, Name: cfg.Agent}, RequestId: cfg.RequestID,
	})
	if err != nil {
		return fmt.Errorf("create Session: %w", err)
	}
	if response.GetSession() == nil {
		return errors.New("create Session returned no Session")
	}
	return writeLifecycleResult(out, format, response, response.GetSession())
}

func deleteSession(
	ctx context.Context,
	client lifecycleClient,
	cfg *DeleteCfg,
	format clioutput.Format,
	out io.Writer,
) error {
	response, err := client.DeleteSession(ctx, &apiv1alpha1.DeleteSessionRequest{
		SessionId: cfg.SessionID,
	})
	if status.Code(err) == codes.Aborted {
		return fmt.Errorf("delete Session: lifecycle work is active or pending; inspect the Session and retry its pending operation: %w", err)
	}
	if err != nil {
		return fmt.Errorf("delete Session: %w", err)
	}
	if response.GetSession() == nil {
		return errors.New("delete Session returned no Session")
	}
	return writeLifecycleResult(out, format, response, response.GetSession())
}

func writeLifecycleResult(
	w io.Writer,
	format clioutput.Format,
	response proto.Message,
	session *apiv1alpha1.Session,
) error {
	if format == clioutput.FormatJSON {
		return clioutput.WriteProto(w, response)
	}
	return writeSessionsTable(w, []*apiv1alpha1.Session{session}, "")
}

// NewCreateCmd constructs the Session create command.
func NewCreateCmd() *cobra.Command {
	cfg := &CreateCfg{}
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Create a Session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			format, err := clioutput.FromCommand(cmd)
			if err != nil {
				return err
			}
			cfg.OutputFormat = format
			return runCreate(cmd.Context(), options, cfg, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cfg.Agent, "agent", "", "Agent name")
	cmd.Flags().StringVar(&cfg.RequestID, "request-id", "", "Idempotency key (generated when omitted)")
	_ = cmd.MarkFlagRequired("agent")
	return cmd
}

// NewDeleteCmd constructs the Session delete command.
func NewDeleteCmd() *cobra.Command {
	cfg := &DeleteCfg{}
	cmd := &cobra.Command{
		Use:   "session ID",
		Short: "Delete a Session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			format, err := clioutput.FromCommand(cmd)
			if err != nil {
				return err
			}
			cfg.OutputFormat = format
			cfg.SessionID = args[0]
			return runDelete(cmd.Context(), options, cfg, cmd.OutOrStdout())
		},
	}
	return cmd
}
