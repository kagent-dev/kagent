//go:build !darwin

package cli

import (
	"context"

	"github.com/abiosoft/ishell/v2"
)

func DashboardCmd(ctx context.Context, c *ishell.Context) {
	c.Println("Dashboard is not available on this platform")
	c.Println("You can easily start the dashboard by running:")
	c.Println("kubectl port-forward -n kagent service/kagent 8082:80")
	c.Println("and then opening http://localhost:8082 in your browser")
}
