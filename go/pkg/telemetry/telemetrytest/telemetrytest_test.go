package telemetrytest

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestFindMetricGaugeShape(t *testing.T) {
	attributes := attribute.NewSet(attribute.String("kagent.gc.stage", "discovery"))
	for _, test := range []struct {
		name string
		data metricdata.Aggregation
	}{
		{name: "integer", data: metricdata.Gauge[int64]{DataPoints: []metricdata.DataPoint[int64]{{Attributes: attributes}}}},
		{name: "floating point", data: metricdata.Gauge[float64]{DataPoints: []metricdata.DataPoint[float64]{{Attributes: attributes}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := metricdata.ResourceMetrics{ScopeMetrics: []metricdata.ScopeMetrics{{Metrics: []metricdata.Metrics{
				{Name: "test.gauge", Unit: "{revision}", Data: test.data},
			}}}}
			shape, found := FindMetric(data, "test.gauge")
			require.True(t, found)
			require.Equal(t, MetricShape{Name: "test.gauge", Kind: "gauge", Unit: "{revision}", AttributeKeys: []string{"kagent.gc.stage"}}, shape)
		})
	}
}
