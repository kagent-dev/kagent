//go:build !darwin

package commands

import (
	"context"
	"fmt"
	"os"
)

func DashboardCmd(ctx context.Context, namespace string) {
	fmt.Fprintln(os.Stderr, "Dashboard is not available on this platform")
	fmt.Fprintln(os.Stderr, "You can easily start the dashboard by running:")
	fmt.Fprintf(os.Stderr, "kubectl port-forward -n %s service/kagent-ui 8082:8080\n", namespace)
	fmt.Fprintln(os.Stderr, "and then opening http://localhost:8082 in your browser")
}
