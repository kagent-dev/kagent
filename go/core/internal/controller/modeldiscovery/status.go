package modeldiscovery

import (
	"context"
	"fmt"
	"math"
	"time"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"

	typed "github.com/kagent-dev/kagent/go/api/clientset/versioned/typed/api/v1alpha3"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	api "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

const (
	// statusWriteTimeout bounds a single Kubernetes status write.
	statusWriteTimeout = 10 * time.Second
	// Failed writes retry independently of provider discovery, matching the other KRT queues.
	statusRetryInitial = time.Second
	statusRetryMax     = 30 * time.Second
)

// Observations contain only status metadata, not model records or credentials.
// Input identity prevents a completed refresh from describing a different source epoch.
type statusObservation struct {
	Input       source
	State       apiv1.DiscoveryState
	Error       string
	Count       int
	LastSuccess *metav1.Time
}

func (o statusObservation) ResourceName() string { return o.Input.ResourceName() }

func (c *Controller) publishStatusLocked(e *entry) {
	o := statusObservation{Input: e.input, State: e.snapshot.State, Error: e.snapshot.Error, Count: len(e.snapshot.Models)}
	if e.snapshot.LastSuccess != nil {
		t := metav1.NewTime(e.snapshot.LastSuccess.AsTime().Truncate(time.Second))
		o.LastSuccess = &t
	}
	c.observations.ConditionalUpdateObject(o)
}

func newStatuses(providers krt.Collection[*api.ModelProviderConfig], inputs krt.Collection[source], observations krt.Collection[statusObservation], opts krt.OptionsBuilder) krt.StatusCollection[*api.ModelProviderConfig, api.ModelProviderConfigStatus] {
	statuses, _ := krt.NewStatusCollection(providers, func(ctx krt.HandlerContext, object *api.ModelProviderConfig) (*api.ModelProviderConfigStatus, *statusObservation) {
		input := krt.FetchOne(ctx, inputs, krt.FilterKey(krt.GetKey(object)))
		observation := krt.FetchOne(ctx, observations, krt.FilterKey(krt.GetKey(object)))
		if input == nil || input.UID != object.UID || input.Generation != object.Generation {
			return nil, nil
		}
		if observation == nil || observation.Input != *input {
			observation = &statusObservation{Input: *input, State: apiv1.DiscoveryState_DISCOVERY_STATE_PENDING}
		}
		status := statusForObservation(*observation)
		return &status, nil
	}, opts.WithName("ModelProviderConfigStatuses")...)
	return statuses
}

func statusForObservation(o statusObservation) api.ModelProviderConfigStatus {
	ready, reason := metav1.ConditionFalse, api.ModelProviderConfigReasonPending
	if o.State == apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT {
		ready, reason = metav1.ConditionTrue, api.ModelProviderConfigReasonDiscovered
	} else if o.Error != "" {
		reason = o.Error
	}
	return api.ModelProviderConfigStatus{ObservedGeneration: o.Input.Generation, ModelCount: o.Count, LastDiscoveryTime: o.LastSuccess,
		Conditions: []metav1.Condition{{Type: api.ModelProviderConfigConditionTypeReady, Status: ready, Reason: reason, Message: reason, ObservedGeneration: o.Input.Generation}}}
}

func statusWithTransitionTimes(desired, current api.ModelProviderConfigStatus) api.ModelProviderConfigStatus {
	// Own only Ready; obsolete conditions are removed without resetting its transition time.
	conditions := []metav1.Condition{}
	if previous := meta.FindStatusCondition(current.Conditions, api.ModelProviderConfigConditionTypeReady); previous != nil {
		conditions = append(conditions, *previous)
	}
	for _, condition := range desired.Conditions {
		meta.SetStatusCondition(&conditions, condition)
	}
	desired.Conditions = conditions
	return desired
}

// StatusWriter consumes the KRT status collection only while this replica leads.
// Queue retries read the latest derived state rather than a captured observation.
type StatusWriter struct {
	discovery *Controller
	client    typed.ApiV1alpha3Interface
}

func NewStatusWriter(discovery *Controller, config *rest.Config) (*StatusWriter, error) {
	client, err := typed.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create model provider status client: %w", err)
	}
	return &StatusWriter{discovery: discovery, client: client}, nil
}
func (s *StatusWriter) NeedLeaderElection() bool { return true }
func (s *StatusWriter) Start(ctx context.Context) error {
	queue := controllers.NewQueue("model-provider-status", controllers.WithGenericReconciler(func(item any) error { return s.write(ctx, item.(string)) }),
		controllers.WithMaxAttempts(math.MaxInt), controllers.WithRateLimiter(workqueue.NewTypedItemExponentialFailureRateLimiter[any](statusRetryInitial, statusRetryMax)))
	registration := s.discovery.statuses.Register(func(event krt.Event[krt.ObjectWithStatus[*api.ModelProviderConfig, api.ModelProviderConfigStatus]]) {
		if event.New == nil {
			return
		}
		desired := event.New
		if !apiequality.Semantic.DeepEqual(statusWithTransitionTimes(desired.Status, desired.Obj.Status), desired.Obj.Status) {
			queue.Add(desired.ResourceName())
		}
	})
	defer registration.UnregisterHandler()
	if !registration.WaitUntilSynced(ctx.Done()) {
		queue.ShutDownEarly()
		return nil
	}
	queue.Run(ctx.Done())
	return nil
}
func (s *StatusWriter) write(ctx context.Context, key string) error {
	desired := s.discovery.statuses.GetKey(key)
	object := s.discovery.providers.GetKey(key)
	if desired == nil || object == nil || desired.Obj.UID != (*object).UID || desired.Obj.Generation != (*object).Generation {
		return nil
	}
	updated := (*object).DeepCopy()
	updated.Status = statusWithTransitionTimes(desired.Status, updated.Status)
	if apiequality.Semantic.DeepEqual(updated.Status, (*object).Status) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, statusWriteTimeout)
	defer cancel()
	_, err := s.client.ModelProviderConfigs(updated.Namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("update ModelProviderConfig %s status: %w", key, err)
	}
	return nil
}
