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
	Connection    *connection.Options
	OutputFormat  string
	Harness       string
	AgentTemplate string
	RequestID     string
}

// DeleteCfg configures AgentInstance deletion.
type DeleteCfg struct {
	Connection   *connection.Options
	OutputFormat string
	InstanceID   string
}

// runCreate creates an AgentInstance.
func runCreate(ctx context.Context, cfg *CreateCfg, out io.Writer) (err error) {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	ensureRequestID(cfg)

	portForward, err := connection.Connect(ctx, cfg.Connection)
	if err != nil {
		return fmt.Errorf("connect to kagent: %w", err)
	}
	if portForward != nil {
		defer portForward.Stop()
	}

	clientSet := cfg.Connection.Client()
	defer func() {
		err = errors.Join(err, clientSet.Close())
	}()
	return create(ctx, clientSet.AgentInstance, cfg.Connection.Namespace, cfg, format, out)
}

func ensureRequestID(cfg *CreateCfg) {
	if cfg.RequestID == "" {
		cfg.RequestID = uuid.NewString()
	}
}

// runDelete deletes an AgentInstance.
func runDelete(ctx context.Context, cfg *DeleteCfg, out io.Writer) (err error) {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	portForward, err := connection.Connect(ctx, cfg.Connection)
	if err != nil {
		return fmt.Errorf("connect to kagent: %w", err)
	}
	if portForward != nil {
		defer portForward.Stop()
	}

	clientSet := cfg.Connection.Client()
	defer func() {
		err = errors.Join(err, clientSet.Close())
	}()
	return deleteAgentInstance(ctx, clientSet.AgentInstance, cfg.Connection.Namespace, cfg, format, out)
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
func NewCreateCmd(connectionOptions *connection.Options, outputFormat *string) *cobra.Command {
	cfg := &CreateCfg{Connection: connectionOptions}
	cmd := &cobra.Command{
		Use:   "agent-instance",
		Short: "Create an AgentInstance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.OutputFormat = *outputFormat
			return runCreate(cmd.Context(), cfg, cmd.OutOrStdout())
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
func NewDeleteCmd(connectionOptions *connection.Options, outputFormat *string) *cobra.Command {
	cfg := &DeleteCfg{Connection: connectionOptions}
	cmd := &cobra.Command{
		Use:   "agent-instance ID",
		Short: "Delete an AgentInstance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.OutputFormat = *outputFormat
			cfg.InstanceID = args[0]
			return runDelete(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}
	return cmd
}
