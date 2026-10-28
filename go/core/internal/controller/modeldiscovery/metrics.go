package modeldiscovery

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// RegisterMetrics is called during initialization, before Start. Only bounded
// state/error categories are labels; source identities and model IDs are not.
func (c *Controller) RegisterMetrics(provider metric.MeterProvider) error {
	meter := provider.Meter("github.com/kagent-dev/kagent/go/core/modeldiscovery")
	duration, err := meter.Float64Histogram("kagent.model_discovery.refresh.duration", metric.WithUnit("s"))
	if err != nil {
		return fmt.Errorf("create discovery duration metric: %w", err)
	}
	count, err := meter.Int64Counter("kagent.model_discovery.refresh.count")
	if err != nil {
		return fmt.Errorf("create discovery count metric: %w", err)
	}
	bytes, err := meter.Int64ObservableGauge("kagent.model_discovery.catalog.bytes", metric.WithUnit("By"))
	if err != nil {
		return fmt.Errorf("create discovery bytes metric: %w", err)
	}
	age, err := meter.Float64ObservableGauge("kagent.model_discovery.catalog.max_age", metric.WithUnit("s"))
	if err != nil {
		return fmt.Errorf("create discovery age metric: %w", err)
	}
	sources, err := meter.Int64ObservableGauge("kagent.model_discovery.sources")
	if err != nil {
		return fmt.Errorf("create discovery source metric: %w", err)
	}
	registration, err := meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		var total int64
		var oldest float64
		counts := map[string]int64{}
		for _, e := range c.entries {
			total += int64(proto.Size(&apiv1.ListProviderModelsResponse{DiscoveredModels: e.snapshot.Models, Endpoint: e.snapshot.Endpoint, State: e.snapshot.State, Error: e.snapshot.Error}))
			counts[e.snapshot.State.String()]++
			if e.snapshot.LastSuccess != nil {
				oldest = max(oldest, time.Since(e.snapshot.LastSuccess.AsTime()).Seconds())
			}
		}
		o.ObserveInt64(bytes, total)
		o.ObserveFloat64(age, oldest)
		for state, count := range counts {
			o.ObserveInt64(sources, count, metric.WithAttributes(attribute.String("state", state)))
		}
		return nil
	}, bytes, age, sources)
	if err != nil {
		return fmt.Errorf("register discovery observations: %w", err)
	}
	c.recordRefresh = func(ctx context.Context, elapsed time.Duration, err error) {
		reason := "success"
		if err != nil {
			reason = err.Error()
		}
		attrs := metric.WithAttributes(attribute.String("result", reason))
		duration.Record(ctx, elapsed.Seconds(), attrs)
		count.Add(ctx, 1, attrs)
	}
	c.unregisterMetrics = registration.Unregister
	return nil
}
