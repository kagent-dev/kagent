package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kagent-dev/kagent/go/api/client"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/spf13/cobra"
)

func newUploadCmd() *cobra.Command {
	var mode uint32
	cmd := &cobra.Command{
		Use: "upload ID LOCAL_FILE REMOTE_PATH", Short: "Stream a local file into a sandbox (replaces the remote file)", Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, func(ctx context.Context, c *client.SandboxClient, _ connection.Options, format clioutput.Format) (err error) {
				file, err := os.Open(args[1])
				if err != nil {
					return err
				}
				defer func() { err = errors.Join(err, file.Close()) }()
				info, err := file.Stat()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() || info.Size() > 64<<20 {
					return errors.New("upload requires a regular file of at most 64 MiB")
				}
				response, err := c.WriteFile(ctx, args[0], args[2], mode, file)
				if err != nil {
					return fmt.Errorf("upload failed; remote file may be partial: %w", err)
				}
				if format == clioutput.FormatJSON {
					return clioutput.WriteProto(cmd.OutOrStdout(), response)
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d bytes to %s\n", response.BytesWritten, args[2])
				return err
			})
		},
	}
	cmd.Flags().Uint32Var(&mode, "mode", 0, "Remote Unix file mode, e.g. 0644 (omission uses guest defaults)")
	return cmd
}

func newDownloadCmd() *cobra.Command {
	return &cobra.Command{
		Use: "download ID REMOTE_PATH LOCAL_FILE", Short: "Download a file, replacing the local destination only after success", Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, func(ctx context.Context, c *client.SandboxClient, _ connection.Options, format clioutput.Format) error {
				err := downloadFile(args[2], func(out io.Writer) error { return c.ReadFile(ctx, args[0], args[1], out) })
				if err != nil {
					return err
				}
				if format == clioutput.FormatJSON {
					return clioutput.WriteJSON(cmd.OutOrStdout(), struct {
						Path string `json:"path"`
					}{Path: args[2]})
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), args[2])
				return err
			})
		},
	}
}

// Stage beside the destination so rename is atomic on the same filesystem.
// A failed transfer leaves any existing destination untouched.
func downloadFile(path string, receive func(io.Writer) error) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".kagent-download-*")
	if err != nil {
		return err
	}
	defer func() {
		removeErr := os.Remove(file.Name())
		if !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	err = receive(file)
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
