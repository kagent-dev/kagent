package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	commonexec "github.com/kagent-dev/kagent/go/core/cli/internal/common/exec"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
)

// statusResources are the kubectl resources kagent status lists, in order.
// WorkerPools are named with their API group so another CRD with the same
// short name cannot shadow Substrate's.
var statusResources = []struct{ title, resource string }{
	{"Pods", "pods"},
	{"WorkerPools", "workerpools.ate.dev"},
}

// runStatus prints each status resource in namespace as kubectl reports it.
// A failed listing does not stop the ones after it: a cluster without the
// WorkerPool CRD still shows its pods, and the command then returns the error.
func runStatus(out io.Writer, namespace string, kubectl func(args ...string) ([]byte, error)) error {
	var failures []error
	for i, r := range statusResources {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s in the %s namespace:\n", r.title, namespace)
		output, err := kubectl("get", r.resource, "-n", namespace)
		// kubectl writes "No resources found" and its errors to the combined
		// output, so print it in both cases.
		if output = bytes.TrimRight(output, "\n"); len(output) > 0 {
			fmt.Fprintf(out, "%s\n", output)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// NewStatusCmd constructs the kagent status command.
func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the pods and worker pools of a kagent installation",
		Long: `Show the pods and Substrate WorkerPools in the kagent namespace, as kubectl
reports them. Use --namespace to check an installation in another namespace.

It reads the cluster through kubectl and the current kubeconfig context, and
does not contact the kagent API, so it works while kagent is still starting.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			kubectl := commonexec.NewKubectlExecutor(options.Verbose, options.Namespace)
			return runStatus(cmd.OutOrStdout(), options.Namespace, kubectl.RunWithOutput)
		},
	}
}
