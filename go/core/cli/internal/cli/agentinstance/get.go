// Package agentinstance implements AgentInstance CLI commands.
package agentinstance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jedib0t/go-pretty/v6/table"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/cli/internal/cli/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/cli/output"
	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxPageSize = 100

type getClient interface {
	GetAgentInstance(context.Context, *apiv1alpha1.GetAgentInstanceRequest) (*apiv1alpha1.GetAgentInstanceResponse, error)
	ListAgentInstances(context.Context, *apiv1alpha1.ListAgentInstancesRequest) (*apiv1alpha1.ListAgentInstancesResponse, error)
}

// GetCfg configures AgentInstance get and list operations.
type GetCfg struct {
	Config     *config.Config
	InstanceID string
	PageSize   int32
	PageToken  string
}

// GetCmd gets one AgentInstance or lists the caller's AgentInstances.
func GetCmd(ctx context.Context, cfg *GetCfg, out io.Writer) (err error) {
	format, err := clioutput.Parse(cfg.Config.OutputFormat)
	if err != nil {
		return err
	}
	if err := validateGetCfg(cfg); err != nil {
		return err
	}

	portForward, err := connection.Connect(ctx, cfg.Config)
	if err != nil {
		return fmt.Errorf("connect to kagent: %w", err)
	}
	if portForward != nil {
		defer portForward.Stop()
	}

	clientSet := cfg.Config.Client()
	defer func() {
		err = errors.Join(err, clientSet.Close())
	}()
	return get(ctx, clientSet.AgentInstance, cfg, format, out)
}

func validateGetCfg(cfg *GetCfg) error {
	if cfg.PageSize < 0 || cfg.PageSize > maxPageSize {
		return fmt.Errorf("page size must be between 1 and %d, or 0 for the server default", maxPageSize)
	}
	if cfg.InstanceID == "" {
		return nil
	}
	instanceID, err := uuid.Parse(cfg.InstanceID)
	if err != nil {
		return fmt.Errorf("invalid AgentInstance ID %q: %w", cfg.InstanceID, err)
	}
	cfg.InstanceID = instanceID.String()
	if cfg.PageSize != 0 || cfg.PageToken != "" {
		return errors.New("pagination flags cannot be used when getting one AgentInstance")
	}
	return nil
}

func get(
	ctx context.Context,
	client getClient,
	cfg *GetCfg,
	format clioutput.Format,
	out io.Writer,
) error {
	if cfg.InstanceID != "" {
		response, err := client.GetAgentInstance(ctx, &apiv1alpha1.GetAgentInstanceRequest{
			Namespace: cfg.Config.Namespace, AgentInstanceId: cfg.InstanceID,
		})
		if err != nil {
			return fmt.Errorf("get AgentInstance: %w", err)
		}
		if response.GetAgentInstance() == nil {
			return errors.New("get AgentInstance returned no AgentInstance")
		}
		if format == clioutput.FormatJSON {
			return clioutput.WriteProto(out, response)
		}
		return writeInstancesTable(out, []*apiv1alpha1.AgentInstance{response.GetAgentInstance()}, "")
	}

	response, err := client.ListAgentInstances(ctx, &apiv1alpha1.ListAgentInstancesRequest{
		Namespace: cfg.Config.Namespace,
		Page:      &apiv1alpha1.PageRequest{Limit: cfg.PageSize, PageToken: cfg.PageToken},
	})
	if err != nil {
		return fmt.Errorf("list AgentInstances: %w", err)
	}
	if response == nil {
		return errors.New("list AgentInstances returned no response")
	}
	if format == clioutput.FormatJSON {
		return clioutput.WriteProto(out, response)
	}
	return writeInstancesTable(out, response.GetAgentInstances(), response.GetPage().GetNextPageToken())
}

func writeInstancesTable(w io.Writer, instances []*apiv1alpha1.AgentInstance, nextPageToken string) error {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"ID", "AGENT TEMPLATE", "HARNESS", "STATE", "CREATED"})
	for _, instance := range instances {
		if instance == nil {
			continue
		}
		tw.AppendRow(table.Row{
			instance.GetId(),
			resourceName(instance.GetAgentTemplate()),
			resourceName(instance.GetHarness()),
			strings.TrimPrefix(instance.GetState().String(), "AGENT_INSTANCE_STATE_"),
			formatTimestamp(instance.GetCreatedAt()),
		})
	}
	output := tw.Render()
	if nextPageToken != "" {
		output += "\nNext page token: " + nextPageToken
	}
	if _, err := fmt.Fprintln(w, output); err != nil {
		return fmt.Errorf("write AgentInstance output: %w", err)
	}
	return nil
}

func resourceName(reference *apiv1alpha1.ResourceReference) string {
	if reference == nil {
		return ""
	}
	return reference.GetName()
}

func formatTimestamp(timestamp *timestamppb.Timestamp) string {
	if timestamp == nil {
		return ""
	}
	return timestamp.AsTime().UTC().Format(time.RFC3339)
}
