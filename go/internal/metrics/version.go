package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/kagent-dev/kagent/go/internal/version"
)

// NewBuildInfoCollector returns a collector that exports metrics about current version
// information.
func NewBuildInfoCollector() prometheus.Collector {
	return prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "kagent_build_info",
			Help: "kagent build metadata exposed as labels with a constant value of 1.",
			ConstLabels: prometheus.Labels{
				"version":    version.Get().Version,
				"git_commit": version.Get().GitCommit,
				"build_date": version.Get().BuildDate,
				"go_version": version.Get().GoVersion,
				"compiler":   version.Get().Compiler,
				"platform":   version.Get().Platform,
			},
		},
		func() float64 { return 1 },
	)
}
