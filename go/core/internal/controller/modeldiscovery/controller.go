package modeldiscovery

import (
	"context"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	api "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/modeldiscovery"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const (
	// refreshInterval balances catalog freshness against polling load, which grows
	// with sources and replicas. One minute is an initial policy default, not an
	// Agw requirement or a measured optimum. Successful refreshes schedule the next
	// poll with ±20% jitter; input changes and explicit refreshes need not wait.
	refreshInterval = time.Minute
	// attemptTimeout bounds each upstream request so a slow endpoint releases its
	// worker slot. Queue time is excluded because the deadline starts at dispatch.
	attemptTimeout = 15 * time.Second
	// explicitTimeout bounds a caller's total refresh wait, including queue time.
	// Expiry ends only that caller's wait, not the shared discovery attempt.
	explicitTimeout = 30 * time.Second
	// explicitCooldown limits new user-triggered attempts after the last attempt
	// started. Calls joining an active attempt bypass this admission check.
	explicitCooldown = 10 * time.Second
	// maxConcurrent caps upstream requests per replica across all sources, bounding
	// outbound load and response memory. Each source also allows one active attempt.
	maxConcurrent = 4
)

// source contains only configuration identity, never credentials. Secret version
// changes invalidate the snapshot even when the source generation is unchanged.
type source struct {
	Ref                                 types.NamespacedName
	UID                                 types.UID
	Generation                          int64
	Type                                api.ModelProvider
	Endpoint, SecretName, SecretVersion string
}

func (s source) ResourceName() string { return s.Ref.String() }

type entry struct {
	input            source
	snapshot         *modeldiscovery.Snapshot
	due, lastAttempt time.Time
	failures         int
	cancel           context.CancelFunc
	done             chan struct{}
}

type Controller struct {
	providers         krt.Collection[*api.ModelProviderConfig]
	observations      krt.StaticCollection[statusObservation]
	statuses          krt.StatusCollection[*api.ModelProviderConfig, api.ModelProviderConfigStatus]
	recordRefresh     func(context.Context, time.Duration, error)
	unregisterMetrics func() error
	mu                sync.Mutex
	entries           map[string]*entry
	observers         map[chan struct{}]struct{}
	inputs            krt.Collection[source]
	secrets           krt.Collection[*corev1.Secret]
	client            *http.Client
	wake              chan struct{}
	running           int
	synced            bool
}

func New(providers krt.Collection[*api.ModelProviderConfig], secrets krt.Collection[*corev1.Secret], opts krt.OptionsBuilder) *Controller {
	inputs := krt.NewCollection(providers, func(ctx krt.HandlerContext, p *api.ModelProviderConfig) *source {
		s := source{Ref: types.NamespacedName{Namespace: p.Namespace, Name: p.Name}, UID: p.UID, Generation: p.Generation, Type: p.Spec.Type, Endpoint: p.Spec.GetEndpoint()}
		if p.Spec.SecretRef != nil {
			s.SecretName = p.Spec.SecretRef.Name
			secret := krt.FetchOne(ctx, secrets, krt.FilterKey(p.Namespace+"/"+s.SecretName))
			if secret != nil {
				s.SecretVersion = string((*secret).UID) + "/" + (*secret).ResourceVersion
			}
			if s.SecretName == "" {
				s.SecretName = "/invalid"
			}
		}
		return &s
	}, opts.WithName("ModelDiscoverySources")...)
	c := &Controller{providers: providers, entries: make(map[string]*entry), observers: make(map[chan struct{}]struct{}), inputs: inputs, secrets: secrets, client: newHTTPClient(), wake: make(chan struct{}, 1)}
	c.observations = krt.NewStaticCollection[statusObservation](nil, nil, opts.WithName("ModelDiscoveryObservations")...)
	c.statuses = newStatuses(providers, inputs, c.observations, opts)
	return c
}

// Discovery is read-only upstream and must run on every API-serving replica.
// Kubernetes status writes are registered separately as a leader-only runnable.
func (c *Controller) NeedLeaderElection() bool { return false }
func (c *Controller) Start(ctx context.Context) error {
	registration := c.inputs.Register(func(event krt.Event[source]) {
		if event.New == nil {
			c.remove(event.Old.ResourceName())
		} else {
			c.observe(*event.New)
		}
	})
	defer registration.UnregisterHandler()
	defer c.client.CloseIdleConnections()
	defer func() {
		if c.unregisterMetrics != nil {
			if err := c.unregisterMetrics(); err != nil {
				logging.FromContext(ctx).ErrorContext(ctx, "failed to unregister discovery metrics", "error", err)
			}
		}
	}()
	if !registration.WaitUntilSynced(ctx.Done()) {
		return nil
	}
	c.mu.Lock()
	c.synced = true
	c.notifyLocked()
	c.mu.Unlock()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var workers sync.WaitGroup
	defer workers.Wait()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, e := range c.entries {
			if e.cancel != nil {
				e.cancel()
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-c.wake:
		}
		c.mu.Lock()
		// Oldest due work wins so a failing source cannot starve another provider.
		due := make([]*entry, 0)
		now := time.Now()
		for _, e := range c.entries {
			if e.cancel == nil && !e.due.After(now) {
				due = append(due, e)
			}
		}
		slices.SortFunc(due, func(a, b *entry) int { return a.due.Compare(b.due) })
		for _, e := range due {
			if c.running >= maxConcurrent {
				break
			}
			attempt, cancel := context.WithTimeout(ctx, attemptTimeout)
			e.cancel = cancel
			e.lastAttempt = now
			c.running++
			workers.Add(1)
			go func(e *entry) { defer workers.Done(); c.refresh(attempt, e) }(e)
		}
		c.mu.Unlock()
	}
}

func pending(s source) *modeldiscovery.Snapshot {
	return &modeldiscovery.Snapshot{Ref: &apiv1.ResourceReference{Namespace: s.Ref.Namespace, Name: s.Ref.Name}, Uid: string(s.UID), Generation: s.Generation, Type: string(s.Type), Endpoint: s.Endpoint, State: apiv1.DiscoveryState_DISCOVERY_STATE_PENDING}
}
func (c *Controller) observe(s source) {
	c.mu.Lock()
	defer c.mu.Unlock()
	old := c.entries[s.ResourceName()]
	if old != nil && old.input == s {
		return
	}
	if old != nil {
		if old.cancel != nil {
			old.cancel()
		}
		close(old.done)
	}
	c.entries[s.ResourceName()] = &entry{input: s, snapshot: pending(s), done: make(chan struct{})}
	c.publishStatusLocked(c.entries[s.ResourceName()])
	c.notifyLocked()
	c.signal()
}
func (c *Controller) remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[key]; e != nil {
		if e.cancel != nil {
			e.cancel()
		}
		close(e.done)
		delete(c.entries, key)
		c.observations.DeleteObject(key)
		c.notifyLocked()
	}
}
func (c *Controller) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}
func (c *Controller) notifyLocked() {
	for ch := range c.observers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (c *Controller) credential(s source) (string, error) {
	if s.SecretName == "" {
		return "", nil
	}
	secret := c.secrets.GetKey(s.Ref.Namespace + "/" + s.SecretName)
	if secret == nil || string((*secret).UID)+"/"+(*secret).ResourceVersion != s.SecretVersion || len((*secret).Data) != 1 {
		return "", errSecret
	}
	for _, value := range (*secret).Data {
		if len(value) == 0 {
			return "", errSecret
		}
		return string(value), nil
	}
	return "", errSecret
}
func (c *Controller) refresh(ctx context.Context, e *entry) {
	started := time.Now()
	var models []*apiv1.DiscoveredModel
	var err error
	if e.input.Type != api.ModelProviderOpenAI {
		err = errUnsupported
	} else {
		var credential string
		credential, err = c.credential(e.input)
		if err == nil {
			models, err = fetch(ctx, c.client, e.input.Endpoint, credential)
		}
	}
	if c.recordRefresh != nil {
		c.recordRefresh(ctx, time.Since(started), err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e.cancel()
	e.cancel = nil
	c.running--
	if c.entries[e.input.ResourceName()] != e {
		c.signal()
		return
	}
	s := pending(e.input)
	s.Models = e.snapshot.Models
	s.LastSuccess = e.snapshot.LastSuccess
	if err == nil {
		s.Models = models
		s.State = apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT
		s.Error = ""
		s.LastSuccess = timestamppb.Now()
		e.failures = 0
		e.due = time.Now().Add(jitter(refreshInterval))
	} else {
		s.Error = err.Error()
		s.State = apiv1.DiscoveryState_DISCOVERY_STATE_FAILED
		if s.LastSuccess != nil {
			s.State = apiv1.DiscoveryState_DISCOVERY_STATE_STALE
		}
		e.failures++
		backoff := 5 * time.Second * time.Duration(1<<min(e.failures-1, 6))
		e.due = time.Now().Add(jitter(min(backoff, 5*time.Minute)))
	}
	changed := s.State != e.snapshot.State || s.Error != e.snapshot.Error || !slices.EqualFunc(s.Models, e.snapshot.Models, func(a, b *apiv1.DiscoveredModel) bool { return proto.Equal(a, b) })
	if !changed {
		s.Models = e.snapshot.Models
	}
	e.snapshot = s
	c.publishStatusLocked(e)
	close(e.done)
	e.done = make(chan struct{})
	if changed {
		c.notifyLocked()
	}
	c.signal()
}
func jitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + rand.Float64()*0.4))
}

func (c *Controller) Refresh(ctx context.Context, ref types.NamespacedName) (*modeldiscovery.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, explicitTimeout)
	defer cancel()
	for {
		c.mu.Lock()
		e := c.entries[ref.String()]
		if e == nil {
			c.mu.Unlock()
			return nil, serviceerrors.NewNotFound("Model provider not found", nil)
		}
		if e.cancel == nil {
			if time.Since(e.lastAttempt) < explicitCooldown {
				c.mu.Unlock()
				return nil, serviceerrors.NewResourceExhausted("Model discovery refresh is rate limited", nil)
			}
			e.due = time.Time{}
			c.signal()
		}
		done := e.done
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
		}
		c.mu.Lock()
		current := c.entries[ref.String()]
		same := current == e
		var s *modeldiscovery.Snapshot
		if same {
			s = e.snapshot.Clone()
		}
		c.mu.Unlock()
		if !same {
			continue
		}
		if s.State != apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT {
			return nil, serviceerrors.NewUnavailable(s.Error, nil)
		}
		return s, nil
	}
}
func (c *Controller) Get(ref types.NamespacedName) *modeldiscovery.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[ref.String()]; e != nil {
		return e.snapshot.Clone()
	}
	return nil
}
func (c *Controller) List() []*modeldiscovery.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*modeldiscovery.Snapshot, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, e.snapshot.Clone())
	}
	slices.SortFunc(out, func(a, b *modeldiscovery.Snapshot) int {
		return strings.Compare(a.Ref.Namespace+"/"+a.Ref.Name, b.Ref.Namespace+"/"+b.Ref.Name)
	})
	return out
}
func (c *Controller) Synced() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.synced }
func (c *Controller) Subscribe() (<-chan struct{}, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan struct{}, 1)
	c.observers[ch] = struct{}{}
	ch <- struct{}{}
	return ch, func() { c.mu.Lock(); defer c.mu.Unlock(); delete(c.observers, ch) }
}
