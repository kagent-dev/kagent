//go:build !darwin

package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func DashboardCmd(ctx context.Context, cfg *config.Config) {
	fmt.Fprintln(os.Stderr, "Dashboard is not available on this platform")
	fmt.Fprintln(os.Stderr, "You can easily start the dashboard by running:")
	fmt.Fprintln(os.Stderr, "kubectl port-forward -n kagent service/kagent-ui 8082:80")
	fmt.Fprintln(os.Stderr, "and then opening http://localhost:8082 in your browser")
}
