// Package session implements Session CLI commands.
package session

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
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxPageSize = 100

type getClient interface {
	GetSession(context.Context, *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error)
	ListSessions(context.Context, *apiv1alpha1.ListSessionsRequest) (*apiv1alpha1.ListSessionsResponse, error)
}

// GetCfg configures Session get and list operations.
type GetCfg struct {
	OutputFormat string
	SessionID    string
	PageSize     int32
	PageToken    string
}

// runGet gets one Session or lists the caller's Sessions.
func runGet(
	ctx context.Context,
	options connection.Options,
	cfg *GetCfg,
	out io.Writer,
) (err error) {
	format, err := clioutput.Parse(cfg.OutputFormat)
	if err != nil {
		return err
	}
	if err := validateGetCfg(cfg); err != nil {
		return err
	}

	session, err := connection.OpenAPI(ctx, options)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, session.Close())
	}()
	return get(ctx, session.API.Session, cfg, format, out)
}

func validateGetCfg(cfg *GetCfg) error {
	if cfg.PageSize < 0 || cfg.PageSize > maxPageSize {
		return fmt.Errorf("page size must be between 1 and %d, or 0 for the server default", maxPageSize)
	}
	if cfg.SessionID == "" {
		return nil
	}
	sessionID, err := uuid.Parse(cfg.SessionID)
	if err != nil {
		return fmt.Errorf("invalid Session ID %q: %w", cfg.SessionID, err)
	}
	cfg.SessionID = sessionID.String()
	if cfg.PageSize != 0 || cfg.PageToken != "" {
		return errors.New("pagination flags cannot be used when getting one Session")
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
	if cfg.SessionID != "" {
		response, err := client.GetSession(ctx, &apiv1alpha1.GetSessionRequest{
			SessionId: cfg.SessionID,
		})
		if err != nil {
			return fmt.Errorf("get Session: %w", err)
		}
		if response.GetSession() == nil {
			return errors.New("get Session returned no Session")
		}
		if format == clioutput.FormatJSON {
			return clioutput.WriteProto(out, response)
		}
		return writeSessionsTable(out, []*apiv1alpha1.Session{response.GetSession()}, "")
	}

	response, err := client.ListSessions(ctx, &apiv1alpha1.ListSessionsRequest{

		Page: &apiv1alpha1.PageRequest{Limit: cfg.PageSize, PageToken: cfg.PageToken},
	})
	if err != nil {
		return fmt.Errorf("list Sessions: %w", err)
	}
	if response == nil {
		return errors.New("list Sessions returned no response")
	}
	if format == clioutput.FormatJSON {
		return clioutput.WriteProto(out, response)
	}
	return writeSessionsTable(out, response.GetSessions(), response.GetPage().GetNextPageToken())
}

func writeSessionsTable(w io.Writer, sessions []*apiv1alpha1.Session, nextPageToken string) error {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"ID", "AGENT", "STATE", "CREATED"})
	for _, session := range sessions {
		if session == nil {
			continue
		}
		tw.AppendRow(table.Row{
			session.GetId(),
			resourceName(session.GetAgent()),
			strings.TrimPrefix(session.GetState().String(), "RUNTIME_STATE_"),
			formatTimestamp(session.GetCreatedAt()),
		})
	}
	output := tw.Render()
	if nextPageToken != "" {
		output += "\nNext page token: " + nextPageToken
	}
	if _, err := fmt.Fprintln(w, output); err != nil {
		return fmt.Errorf("write Session output: %w", err)
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

// NewGetCmd constructs the Session get/list command.
func NewGetCmd() *cobra.Command {
	cfg := &GetCfg{}
	cmd := &cobra.Command{
		Use:   "session [ID]",
		Short: "Get a Session or list your Sessions",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			format, err := clioutput.FromCommand(cmd)
			if err != nil {
				return err
			}
			var sessionID string
			if len(args) == 1 {
				sessionID = args[0]
			}
			cfg.OutputFormat = format
			cfg.SessionID = sessionID
			return runGet(cmd.Context(), options, cfg, cmd.OutOrStdout())
		},
	}
	cmd.Flags().Int32Var(&cfg.PageSize, "page-size", 0, "Number of Sessions to return (default 50, maximum 100)")
	cmd.Flags().StringVar(&cfg.PageToken, "page-token", "", "Token returned by the previous page")
	return cmd
}
