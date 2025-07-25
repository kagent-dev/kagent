package goruntime

import (
	"fmt"
	"runtime/debug"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/go-logr/logr"
)

func SetMemLimit(logger logr.Logger, memlimitRatio float64) {
	if memlimitRatio >= 1.0 {
		memlimitRatio = 1.0
	} else if memlimitRatio <= 0.0 {
		memlimitRatio = 0.0
	}

	// the memlimitRatio argument to 0, effectively disabling auto memory limit for all users.
	if memlimitRatio == 0.0 {
		return
	}

	if _, err := memlimit.SetGoMemLimitWithOpts(
		memlimit.WithRatio(memlimitRatio),
		memlimit.WithProvider(
			memlimit.ApplyFallback(
				memlimit.FromCgroup,
				memlimit.FromSystem,
			),
		),
	); err != nil {
		logger.Error(err, "Failed to set GOMEMLIMIT automatically", "component", "automemlimit")
	}

	logger.Info(fmt.Sprintf("GOMEMLIMIT set to %d", debug.SetMemoryLimit(-1)))
}
