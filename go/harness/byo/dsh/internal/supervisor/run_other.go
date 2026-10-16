//go:build !linux

package supervisor

import (
	"fmt"
	"os"
)

func Run([]string) int {
	fmt.Fprintln(os.Stderr, "DSH BYO process supervision requires Linux")
	return 1
}
