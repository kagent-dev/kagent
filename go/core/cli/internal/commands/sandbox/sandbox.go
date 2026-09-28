// Package sandbox implements standalone sandbox commands.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/kagent-dev/kagent/go/api/client"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/durationpb"
)

// NewCmd constructs commands using the CLI's shared API connection and identity.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sandbox", Short: "Run commands and transfer files in standalone sandboxes"}
	cmd.AddCommand(newTemplatesCmd(), newCreateCmd(), newListCmd(), newExecCmd(), newWaitCmd(), newProcessCmd(), newKillCmd(), newUploadCmd(), newDownloadCmd())
	for _, action := range []string{"get", "suspend", "resume", "delete"} {
		cmd.AddCommand(newLifecycleCmd(action))
	}
	return cmd
}

func withClient(cmd *cobra.Command, run func(context.Context, *client.SandboxClient, connection.Options, clioutput.Format) error) (err error) {
	options, err := connection.OptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	value, err := clioutput.FromCommand(cmd)
	if err != nil {
		return err
	}
	format, err := clioutput.Parse(value)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}
	session, err := connection.OpenAPI(ctx, options)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	return run(ctx, session.API.Sandbox, options, format)
}

func newTemplatesCmd() *cobra.Command {
	return &cobra.Command{
		Use: "templates", Short: "List SandboxTemplates in the selected namespace", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withClient(cmd, func(ctx context.Context, c *client.SandboxClient, options connection.Options, format clioutput.Format) error {
				response, err := c.ListSandboxTemplates(ctx, &apiv1alpha1.ListSandboxTemplatesRequest{Namespace: options.Namespace})
				if err != nil {
					return err
				}
				if format == clioutput.FormatJSON {
					return clioutput.WriteProto(cmd.OutOrStdout(), response)
				}
				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				if _, err := fmt.Fprintln(w, "NAMESPACE\tNAME\tIMAGE"); err != nil {
					return err
				}
				for _, template := range response.SandboxTemplates {
					if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", template.Ref.GetNamespace(), template.Ref.GetName(), template.WorkloadImage); err != nil {
						return err
					}
				}
				return w.Flush()
			})
		},
	}
}

func newCreateCmd() *cobra.Command {
	var requestID, name string
	var ttl time.Duration
	cmd := &cobra.Command{
		Use: "create TEMPLATE --request-id ID", Short: "Create a sandbox from a prepared template", Args: cobra.ExactArgs(1),
		Long: "Create a sandbox. Retain --request-id and all inputs for retries; each call makes one lifecycle attempt. Activity does not extend the TTL.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if requestID == "" {
				return errors.New("request-id must not be empty")
			}
			if cmd.Flags().Changed("ttl") && (ttl < time.Second || ttl > 24*time.Hour) {
				return errors.New("ttl must be between 1s and 24h")
			}
			return withClient(cmd, func(ctx context.Context, c *client.SandboxClient, options connection.Options, format clioutput.Format) error {
				request := &apiv1alpha1.CreateSandboxRequest{
					SandboxTemplate: &apiv1alpha1.ResourceReference{Namespace: options.Namespace, Name: args[0]}, RequestId: requestID, Name: name,
				}
				if cmd.Flags().Changed("ttl") {
					request.Ttl = durationpb.New(ttl)
				}
				response, err := c.CreateSandbox(ctx, request)
				if err != nil {
					return fmt.Errorf("create sandbox (retry with request-id %q and identical inputs): %w", requestID, err)
				}
				return writeSandbox(cmd.OutOrStdout(), format, response.GetSandbox())
			})
		},
	}
	cmd.Flags().StringVar(&requestID, "request-id", "", "Stable idempotency key; reuse with identical inputs for retries")
	cmd.Flags().StringVar(&name, "name", "", "Display name")
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "Lifetime (omission uses operator policy)")
	_ = cmd.MarkFlagRequired("request-id")
	return cmd
}

func newListCmd() *cobra.Command {
	var pageSize int32
	var pageToken string
	cmd := &cobra.Command{
		Use: "list", Short: "List your sandboxes", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withClient(cmd, func(ctx context.Context, c *client.SandboxClient, _ connection.Options, format clioutput.Format) error {
				response, err := c.ListSandboxes(ctx, &apiv1alpha1.ListSandboxesRequest{Page: &apiv1alpha1.PageRequest{Limit: pageSize, PageToken: pageToken}})
				if err != nil {
					return err
				}
				if format == clioutput.FormatJSON {
					return clioutput.WriteProto(cmd.OutOrStdout(), response)
				}
				if err := writeSandboxesTable(cmd.OutOrStdout(), response.Sandboxes); err != nil {
					return err
				}
				if next := response.GetPage().GetNextPageToken(); next != "" {
					_, err = fmt.Fprintf(cmd.ErrOrStderr(), "Next page: --page-token %s\n", next)
				}
				return err
			})
		},
	}
	cmd.Flags().Int32Var(&pageSize, "page-size", 0, "Maximum number of sandboxes")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "Continuation token from list")
	return cmd
}

func newLifecycleCmd(action string) *cobra.Command {
	return &cobra.Command{
		Use: action + " ID", Short: action + " a sandbox", Args: cobra.ExactArgs(1),
		Long: "Inspect or change sandbox lifecycle. Mutations make one attempt; retry the same mutation on transient errors. Get only observes. Suspend can interrupt work; delete removes files.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, func(ctx context.Context, c *client.SandboxClient, _ connection.Options, format clioutput.Format) error {
				var value *apiv1alpha1.Sandbox
				var err error
				switch action {
				case "get":
					var response *apiv1alpha1.GetSandboxResponse
					response, err = c.GetSandbox(ctx, &apiv1alpha1.GetSandboxRequest{SandboxId: args[0]})
					value = response.GetSandbox()
				case "suspend":
					var response *apiv1alpha1.SuspendSandboxResponse
					response, err = c.SuspendSandbox(ctx, &apiv1alpha1.SuspendSandboxRequest{SandboxId: args[0]})
					value = response.GetSandbox()
				case "resume":
					var response *apiv1alpha1.ResumeSandboxResponse
					response, err = c.ResumeSandbox(ctx, &apiv1alpha1.ResumeSandboxRequest{SandboxId: args[0]})
					value = response.GetSandbox()
				case "delete":
					var response *apiv1alpha1.DeleteSandboxResponse
					response, err = c.DeleteSandbox(ctx, &apiv1alpha1.DeleteSandboxRequest{SandboxId: args[0]})
					value = response.GetSandbox()
				default:
					return fmt.Errorf("unknown sandbox action %q", action)
				}
				if err != nil {
					return fmt.Errorf("%s sandbox %s: %w", action, args[0], err)
				}
				return writeSandbox(cmd.OutOrStdout(), format, value)
			})
		},
	}
}

func writeSandbox(out io.Writer, format clioutput.Format, value *apiv1alpha1.Sandbox) error {
	if value == nil {
		return errors.New("server returned no sandbox")
	}
	if format == clioutput.FormatJSON {
		return clioutput.WriteProto(out, value)
	}
	return writeSandboxesTable(out, []*apiv1alpha1.Sandbox{value})
}

func writeSandboxesTable(out io.Writer, values []*apiv1alpha1.Sandbox) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tTEMPLATE\tSTATE\tOPERATION\tEXPIRES\tFAILURE"); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(w, "%s\t%s/%s\t%s\t%s\t%s\t%s\n", value.Id, value.SandboxTemplate.GetNamespace(), value.SandboxTemplate.GetName(), value.State, value.Operation, value.ExpiresAt.AsTime().Format(time.RFC3339), value.Failure.GetMessage()); err != nil {
			return err
		}
	}
	return w.Flush()
}
