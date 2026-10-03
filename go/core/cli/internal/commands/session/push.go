package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/spf13/cobra"
)

type pushOperation string

const (
	pushCreate pushOperation = "create"
	pushGet    pushOperation = "get"
	pushList   pushOperation = "list"
	pushDelete pushOperation = "delete"
)

type pushCfg struct {
	URL              string
	ID               string
	Token            string
	TokenFile        string
	BearerFile       string
	BearerCredential string
	PageSize         int32
	PageToken        string
}

type taskPushClient interface {
	CreateTaskPushConfig(context.Context, *a2a.PushConfig) (*a2a.PushConfig, error)
	GetTaskPushConfig(context.Context, *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error)
	DeleteTaskPushConfig(context.Context, *a2a.DeleteTaskPushConfigRequest) error
}

type pushPageClient interface {
	ListTaskPushConfigsPage(context.Context, string, string, int32, string) (*a2apb.ListTaskPushNotificationConfigsResponse, error)
}

func newPushCmd() *cobra.Command {
	root := &cobra.Command{Use: "push", Short: "Manage A2A task push callbacks"}
	for _, op := range []pushOperation{pushCreate, pushGet, pushList, pushDelete} {
		cfg := &pushCfg{}
		command := &cobra.Command{
			Use:   string(op) + " SESSION_ID TASK_ID",
			Short: pushCommandShort(op),
			Args: func(cmd *cobra.Command, args []string) error {
				want := 2
				if op == pushGet || op == pushDelete {
					want = 3
				}
				return cobra.ExactArgs(want)(cmd, args)
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				options, err := connection.OptionsFromCommand(cmd)
				if err != nil {
					return err
				}
				formatName, err := clioutput.FromCommand(cmd)
				if err != nil {
					return err
				}
				format, err := clioutput.Parse(formatName)
				if err != nil {
					return err
				}
				return runPush(cmd.Context(), options, op, args, cfg, format, cmd.OutOrStdout())
			},
		}
		if op == pushGet || op == pushDelete {
			command.Use += " CONFIG_ID"
		}
		if op == pushCreate {
			command.Flags().StringVar(&cfg.URL, "url", "", "HTTP or HTTPS callback URL")
			command.Flags().StringVar(&cfg.ID, "id", "", "Callback ID (generated when omitted)")
			command.Flags().StringVar(&cfg.TokenFile, "token-file", "", "Read the optional notification token from a file")
			command.Flags().StringVar(&cfg.BearerFile, "bearer-token-file", "", "Read an optional webhook Bearer credential from a file")
			_ = command.MarkFlagRequired("url")
		}
		if op == pushList {
			command.Flags().Int32Var(&cfg.PageSize, "page-size", 0, "Callbacks per page (default 50, maximum 100)")
			command.Flags().StringVar(&cfg.PageToken, "page-token", "", "Token returned by the previous page")
		}
		root.AddCommand(command)
	}
	return root
}

func pushCommandShort(op pushOperation) string {
	switch op {
	case pushCreate:
		return "Add or replace a task callback"
	case pushGet:
		return "Get a task callback"
	case pushList:
		return "List active task callbacks"
	case pushDelete:
		return "Remove a task callback"
	default:
		return "Manage a task callback"
	}
}

func runPush(ctx context.Context, options connection.Options, op pushOperation, args []string, cfg *pushCfg, format clioutput.Format, out io.Writer) (err error) {
	wantArgs := 2
	if op == pushGet || op == pushDelete {
		wantArgs = 3
	}
	if len(args) != wantArgs {
		if wantArgs == 3 {
			return fmt.Errorf("%s requires a Session ID, task ID, and callback ID", op)
		}
		return fmt.Errorf("%s requires a Session ID and task ID", op)
	}
	sessionID, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid Session ID %q: %w", args[0], err)
	}
	if args[1] == "" {
		return errors.New("task ID must not be empty")
	}
	if wantArgs == 3 && args[2] == "" {
		return errors.New("callback ID must not be empty")
	}
	if op == pushCreate {
		if err := validatePushURL(cfg.URL); err != nil {
			return err
		}
		if cfg.TokenFile != "" {
			token, err := readPushToken(cfg.TokenFile)
			if err != nil {
				return err
			}
			cfg.Token = token
		}
		if cfg.BearerFile != "" {
			cfg.BearerCredential, err = readPushToken(cfg.BearerFile)
			if err != nil {
				return err
			}
		}
	}
	if op == pushList && (cfg.PageSize < 0 || cfg.PageSize > 100) {
		return errors.New("page size must be between 1 and 100, or 0 for the server default")
	}
	session, err := connection.OpenGateway(ctx, options)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	if op == pushList {
		return executePush(ctx, nil, session.Gateway.A2A, op, sessionID.String(), args[1], args[2:], cfg, format, out)
	}
	client, err := session.Gateway.A2A.ForSession(ctx, sessionID.String())
	if err != nil {
		return fmt.Errorf("create Session A2A client: %w", err)
	}
	return executePush(ctx, client, session.Gateway.A2A, op, sessionID.String(), args[1], args[2:], cfg, format, out)
}

func readPushToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read notification token file: %w", err)
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
}

func pushBearerAuth(credential string) *a2a.PushAuthInfo {
	if credential == "" {
		return nil
	}
	return &a2a.PushAuthInfo{Scheme: "Bearer", Credentials: credential}
}

func executePush(ctx context.Context, client taskPushClient, pages pushPageClient, op pushOperation, sessionID, taskID string, extra []string, cfg *pushCfg, format clioutput.Format, out io.Writer) error {
	switch op {
	case pushCreate:
		config, err := client.CreateTaskPushConfig(ctx, &a2a.PushConfig{TaskID: a2a.TaskID(taskID), ID: cfg.ID, URL: cfg.URL, Token: cfg.Token, Auth: pushBearerAuth(cfg.BearerCredential)})
		if err != nil {
			return fmt.Errorf("create task push callback: %w", err)
		}
		return writePushConfig(out, format, config)
	case pushGet:
		config, err := client.GetTaskPushConfig(ctx, &a2a.GetTaskPushConfigRequest{TaskID: a2a.TaskID(taskID), ID: extra[0]})
		if err != nil {
			return fmt.Errorf("get task push callback: %w", err)
		}
		return writePushConfig(out, format, config)
	case pushList:
		page, err := pages.ListTaskPushConfigsPage(ctx, sessionID, taskID, cfg.PageSize, cfg.PageToken)
		if err != nil {
			return fmt.Errorf("list task push callbacks: %w", err)
		}
		if format == clioutput.FormatJSON {
			return clioutput.WriteProto(out, page)
		}
		tw := table.NewWriter()
		tw.AppendHeader(table.Row{"ID", "CALLBACK URL"})
		for _, config := range page.GetConfigs() {
			tw.AppendRow(table.Row{config.GetId(), config.GetUrl()})
		}
		if _, err := fmt.Fprintln(out, tw.Render()); err != nil {
			return err
		}
		if page.GetNextPageToken() != "" {
			_, err = fmt.Fprintln(out, "Next page token: "+page.GetNextPageToken())
		}
		return err
	case pushDelete:
		if err := client.DeleteTaskPushConfig(ctx, &a2a.DeleteTaskPushConfigRequest{TaskID: a2a.TaskID(taskID), ID: extra[0]}); err != nil {
			return fmt.Errorf("delete task push callback: %w", err)
		}
		if format == clioutput.FormatJSON {
			return clioutput.WriteJSON(out, map[string]string{"taskId": taskID, "id": extra[0], "status": "deleted"})
		}
		_, err := fmt.Fprintf(out, "Deleted callback %s from task %s.\n", extra[0], taskID)
		return err
	default:
		return fmt.Errorf("unknown push operation %q", op)
	}
}

func writePushConfig(out io.Writer, format clioutput.Format, config *a2a.PushConfig) error {
	if config == nil {
		return errors.New("server returned no push configuration")
	}
	if format == clioutput.FormatJSON {
		return clioutput.WriteJSON(out, config)
	}
	_, err := fmt.Fprintf(out, "Task: %s\nCallback ID: %s\nURL: %s\n", config.TaskID, config.ID, config.URL)
	return err
}

func validatePushURL(raw string) error {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.Fragment != "" || strings.TrimSpace(raw) != raw {
		return errors.New("push URL must be an absolute HTTP or HTTPS URL without credentials")
	}
	return nil
}

var _ taskPushClient = (*a2aclient.Client)(nil)
