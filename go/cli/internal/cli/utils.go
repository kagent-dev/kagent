package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/pkg/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func CheckServerConnection(client *client.ClientSet) error {
	// Only check if we have a valid client
	if client == nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	_, err := client.Version.GetVersion(ctx)
	if err != nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}
	return nil
}

type portForward struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func NewPortForward(ctx context.Context, cfg *config.Config) *portForward {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "kubectl", "-n", cfg.Namespace, "port-forward", "service/kagent-controller", "8083:8083")
	// Error connecting to server, port-forward the server
	go func() {
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			os.Exit(1)
		}
	}()

	client := client.New(cfg.APIURL)
	// Try to connect 5 times
	for i := 0; i < 5; i++ {
		if err := CheckServerConnection(client); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	return &portForward{
		cmd:    cmd,
		cancel: cancel,
	}
}

func (p *portForward) Stop() {
	p.cancel()
	if err := p.cmd.Wait(); err != nil {
		if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "exit status 1") {
			fmt.Fprintf(os.Stderr, "Error waiting for port-forward to exit: %v\n", err)
		}
	}
}

func StreamA2AEvents(ch <-chan protocol.StreamingMessageEvent, verbose bool) {
	for event := range ch {
		if verbose {
			json, err := event.MarshalJSON()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling A2A event: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stdout, "%+v\n", string(json))
		} else {
			json, err := event.MarshalJSON()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling A2A event: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stdout, "%+v\n", string(json))
		}
	}
	fmt.Fprintln(os.Stdout) // Add a newline after streaming is complete
}

func startPortForward(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	a2aPortFwdCmd := exec.CommandContext(ctx, "kubectl", "-n", "kagent", "port-forward", "service/kagent", "8083:8083")
	// Error connecting to server, port-forward the server
	go func() {
		if err := a2aPortFwdCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			os.Exit(1)
		}
	}()

	// Ensure the context is cancelled when the shell is closed
	return func() {
		cancel()
		if err := a2aPortFwdCmd.Wait(); err != nil {
			// These 2 errors are expected
			if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "exec: not started") {
				fmt.Fprintf(os.Stderr, "Error waiting for port-forward to exit: %v\n", err)
			}
		}
	}
}
