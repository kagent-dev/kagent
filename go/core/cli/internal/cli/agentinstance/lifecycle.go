package agentinstance

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/cli/internal/cli/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/cli/output"
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

// CreateCmd creates an AgentInstance.
func CreateCmd(ctx context.Context, cfg *CreateCfg, out io.Writer) (err error) {
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

// DeleteCmd deletes an AgentInstance.
func DeleteCmd(ctx context.Context, cfg *DeleteCfg, out io.Writer) (err error) {
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
