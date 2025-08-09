package goruntime

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"go.uber.org/automaxprocs/maxprocs"
)

func SetMaxProcs(logger logr.Logger) {
	l := func(format string, a ...interface{}) {
		logger.Info(fmt.Sprintf(strings.TrimPrefix(format, "maxprocs: "), a...))
	}

	if _, err := maxprocs.Set(maxprocs.Logger(l)); err != nil {
		logger.Error(err, "Failed to set GOMAXPROCS automatically")
	}
}
