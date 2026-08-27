package agentinstance

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
	CreateAgentInstance(context.Context, *apiv1alpha1.CreateAgentInstanceRequest) (*apiv1alpha1.CreateAgentInstanceResponse, error)
	DeleteAgentInstance(context.Context, *apiv1alpha1.DeleteAgentInstanceRequest) (*apiv1alpha1.DeleteAgentInstanceResponse, error)
}

// CreateCfg configures AgentInstance creation.
type CreateCfg struct {
	OutputFormat  string
	Harness       string
	AgentTemplate string
	RequestID     string
}

// DeleteCfg configures AgentInstance deletion.
type DeleteCfg struct {
	OutputFormat string
	InstanceID   string
}

// runCreate creates an AgentInstance.
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

	session, err := connection.Open(ctx, options)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, session.Close())
	}()
	return create(ctx, session.Client.AgentInstance, session.Namespace, cfg, format, out)
}

func ensureRequestID(cfg *CreateCfg) {
	if cfg.RequestID == "" {
		cfg.RequestID = uuid.NewString()
	}
}

// runDelete deletes an AgentInstance.
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
	session, err := connection.Open(ctx, options)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, session.Close())
	}()
	return deleteAgentInstance(ctx, session.Client.AgentInstance, session.Namespace, cfg, format, out)
}

func create(
	ctx context.Context,
	client lifecycleClient,
	namespace string,
	cfg *CreateCfg,
	format clioutput.Format,
	out io.Writer,
) error {
	response, err := client.CreateAgentInstance(ctx, &apiv1alpha1.CreateAgentInstanceRequest{
		Namespace: namespace, Harness: cfg.Harness,
		AgentTemplate: cfg.AgentTemplate, RequestId: cfg.RequestID,
	})
	if err != nil {
		return fmt.Errorf("create AgentInstance: %w", err)
	}
	if response.GetAgentInstance() == nil {
		return errors.New("create AgentInstance returned no AgentInstance")
	}
	return writeLifecycleResult(out, format, response, response.GetAgentInstance())
}

func deleteAgentInstance(
	ctx context.Context,
	client lifecycleClient,
	namespace string,
	cfg *DeleteCfg,
	format clioutput.Format,
	out io.Writer,
) error {
	response, err := client.DeleteAgentInstance(ctx, &apiv1alpha1.DeleteAgentInstanceRequest{
		Namespace: namespace, AgentInstanceId: cfg.InstanceID,
	})
	if status.Code(err) == codes.Aborted {
		return fmt.Errorf("delete AgentInstance: another lifecycle operation is in progress; retry after it completes: %w", err)
	}
	if err != nil {
		return fmt.Errorf("delete AgentInstance: %w", err)
	}
	if response.GetAgentInstance() == nil {
		return errors.New("delete AgentInstance returned no AgentInstance")
	}
	return writeLifecycleResult(out, format, response, response.GetAgentInstance())
}

func writeLifecycleResult(
	w io.Writer,
	format clioutput.Format,
	response proto.Message,
	instance *apiv1alpha1.AgentInstance,
) error {
	if format == clioutput.FormatJSON {
		return clioutput.WriteProto(w, response)
	}
	return writeInstancesTable(w, []*apiv1alpha1.AgentInstance{instance}, "")
}

// NewCreateCmd constructs the AgentInstance create command.
func NewCreateCmd() *cobra.Command {
	cfg := &CreateCfg{}
	cmd := &cobra.Command{
		Use:   "agent-instance",
		Short: "Create an AgentInstance",
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
	cmd.Flags().StringVar(&cfg.Harness, "harness", "", "Harness name")
	cmd.Flags().StringVar(&cfg.AgentTemplate, "agent-template", "", "AgentTemplate name")
	cmd.Flags().StringVar(&cfg.RequestID, "request-id", "", "Idempotency key (generated when omitted)")
	_ = cmd.MarkFlagRequired("harness")
	_ = cmd.MarkFlagRequired("agent-template")
	return cmd
}

// NewDeleteCmd constructs the AgentInstance delete command.
func NewDeleteCmd() *cobra.Command {
	cfg := &DeleteCfg{}
	cmd := &cobra.Command{
		Use:   "agent-instance ID",
		Short: "Delete an AgentInstance",
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
			cfg.InstanceID = args[0]
			return runDelete(cmd.Context(), options, cfg, cmd.OutOrStdout())
		},
	}
	return cmd
}
