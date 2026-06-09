package a2a

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"reflect"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agent_translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	common "github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type A2ARegistrar struct {
	cache          crcache.Cache
	handlerMux     A2AHandlerMux
	clientRegistry *AgentClientRegistry
	a2aBaseURL     string
	sandboxA2AURL  string
	authenticator  auth.AuthProvider
	agentObserver  AgentObserver
}

type AgentObserver interface {
	NotifyAgentsChanged(ctx context.Context)
}

var _ manager.Runnable = (*A2ARegistrar)(nil)

func NewA2ARegistrar(
	cache crcache.Cache,
	mux A2AHandlerMux,
	clientRegistry *AgentClientRegistry,
	a2aBaseUrl string,
	sandboxA2ABaseURL string,
	authenticator auth.AuthProvider,
	agentObserver AgentObserver,
) (*A2ARegistrar, error) {
	if clientRegistry == nil {
		return nil, fmt.Errorf("clientRegistry must not be nil")
	}
	reg := &A2ARegistrar{
		cache:          cache,
		handlerMux:     mux,
		clientRegistry: clientRegistry,
		a2aBaseURL:     a2aBaseUrl,
		sandboxA2AURL:  sandboxA2ABaseURL,
		authenticator:  authenticator,
		agentObserver:  agentObserver,
	}

	return reg, nil
}

func (a *A2ARegistrar) NeedLeaderElection() bool {
	return false
}

func (a *A2ARegistrar) Start(ctx context.Context) error {
	log := ctrllog.FromContext(ctx).WithName("a2a-registrar")

	if err := a.registerAgentInformer(ctx, &v1alpha2.Agent{}, log); err != nil {
		return err
	}
	if err := a.registerAgentInformer(ctx, &v1alpha2.SandboxAgent{}, log); err != nil {
		return err
	}

	if ok := a.cache.WaitForCacheSync(ctx); !ok {
		return fmt.Errorf("cache sync failed")
	}

	<-ctx.Done()
	return nil
}

func (a *A2ARegistrar) registerAgentInformer(ctx context.Context, prototype v1alpha2.AgentObject, log logr.Logger) error {
	informer, err := a.cache.GetInformer(ctx, prototype)
	if err != nil {
		return fmt.Errorf("failed to get cache informer for %T: %w", prototype, err)
	}

	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			agent, ok := informerAgentObject(obj)
			if !ok {
				return
			}
			if err := a.upsertAgentHandler(ctx, agent, log); err != nil {
				log.Error(err, "failed to upsert A2A handler", "agent", common.GetObjectRef(agent))
				return
			}
			a.notifyAgentChange(ctx)
		},
		UpdateFunc: func(oldObj, newObj any) {
			oldAgent, ok1 := informerAgentObject(oldObj)
			newAgent, ok2 := informerAgentObject(newObj)
			if !ok1 || !ok2 {
				return
			}
			specChanged := oldAgent.GetGeneration() != newAgent.GetGeneration() || !sameAgentSpec(oldAgent, newAgent)
			if specChanged {
				if err := a.upsertAgentHandler(ctx, newAgent, log); err != nil {
					log.Error(err, "failed to upsert A2A handler", "agent", common.GetObjectRef(newAgent))
					return
				}
			}
			// Also notify when readiness conditions change so subscribers don't
			// hold stale agent lists (the resource filter uses Accepted +
			// DeploymentReady, which are status conditions, not spec fields).
			if specChanged || agentReadinessChanged(oldAgent, newAgent) {
				a.notifyAgentChange(ctx)
			}
		},
		DeleteFunc: func(obj any) {
			agent, ok := deletedInformerAgentObject(obj)
			if !ok {
				return
			}
			ref := a2aRouteKey(agent)
			a.handlerMux.RemoveAgentHandler(ref)
			a.clientRegistry.delete(ref)
			log.V(1).Info("removed A2A handler", "agent", ref)
			a.notifyAgentChange(ctx)
		},
	}); err != nil {
		return fmt.Errorf("failed to add informer event handler for %T: %w", prototype, err)
	}

	return nil
}

func (a *A2ARegistrar) notifyAgentChange(ctx context.Context) {
	if a.agentObserver != nil {
		a.agentObserver.NotifyAgentsChanged(ctx)
	}
}

func agentReadinessChanged(oldAgent, newAgent v1alpha2.AgentObject) bool {
	return isAgentReady(oldAgent) != isAgentReady(newAgent)
}

func isAgentReady(agent v1alpha2.AgentObject) bool {
	status := agent.GetAgentStatus()
	if status == nil {
		return false
	}
	deploymentReady, accepted := false, false
	for _, c := range status.Conditions {
		if c.Type == "Ready" && c.Reason == "DeploymentReady" && string(c.Status) == "True" {
			deploymentReady = true
		}
		if c.Type == "Accepted" && string(c.Status) == "True" {
			accepted = true
		}
	}
	return deploymentReady && accepted
}

func sameAgentSpec(oldAgent, newAgent v1alpha2.AgentObject) bool {
	oldSpec := oldAgent.GetAgentSpec()
	newSpec := newAgent.GetAgentSpec()
	switch {
	case oldSpec == nil && newSpec == nil:
		return true
	case oldSpec == nil || newSpec == nil:
		return false
	default:
		return reflect.DeepEqual(oldSpec, newSpec)
	}
}

func informerAgentObject(obj any) (v1alpha2.AgentObject, bool) {
	typed, ok := obj.(v1alpha2.AgentObject)
	return typed, ok
}

func deletedInformerAgentObject(obj any) (v1alpha2.AgentObject, bool) {
	if typed, ok := informerAgentObject(obj); ok {
		return typed, true
	}
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil, false
	}
	return informerAgentObject(tombstone.Obj)
}

func (a *A2ARegistrar) upsertAgentHandler(ctx context.Context, agent v1alpha2.AgentObject, log logr.Logger) error {
	agentRef := types.NamespacedName{Namespace: agent.GetNamespace(), Name: agent.GetName()}
	card := agent_translator.GetA2AAgentCard(agent)

	provider := resolveProviderName(ctx, a.cache, agent)

	httpClient := debugHTTPClient()
	client, err := a2aclient.NewFromEndpoints(
		ctx,
		// TODO(0.11.0): Prefer A2A 1.0 interfaces by default once managed runtimes are v1-capable.
		// Keep legacy fallback during rollout so old agent pods continue to serve traffic.
		filterInterfacesByVersion(card.SupportedInterfaces, a2atype.ProtocolVersion("0.3")),
		a2aclient.WithJSONRPCTransport(httpClient),
		// TODO(0.11.0): Remove the compat transport after legacy runtimes are unsupported.
		a2aclient.WithCompatTransport(
			a2atype.ProtocolVersion("0.3"),
			a2atype.TransportProtocolJSONRPC,
			// This creates a legacy JSON-RPC transport that is used to forward traffic to agents that are still on the legacy A2A wire.
			a2aclient.TransportFactoryFn(func(_ context.Context, _ *a2atype.AgentCard, iface *a2atype.AgentInterface) (a2aclient.Transport, error) {
				return a2av0.NewJSONRPCTransport(a2av0.JSONRPCTransportConfig{
					URL:    iface.URL,
					Client: httpClient,
				}), nil
			}),
		),
		a2aclient.WithCallInterceptors(
			NewUpstreamAuthInterceptor(a.authenticator, agentRef),
		),
	)
	if err != nil {
		return fmt.Errorf("create A2A client for %s: %w", agentRef, err)
	}

	cardCopy := *card
	cardCopy.SupportedInterfaces = cloneInterfacesWithURL(card.SupportedInterfaces, a.a2aRouteURL(agent))

	routeRef := a2aRouteKey(agent)
	if err := a.handlerMux.SetAgentHandler(routeRef, client, cardCopy, newA2ATracingMiddleware(agentRef, provider)); err != nil {
		return fmt.Errorf("set handler for %s: %w", agentRef, err)
	}

	a.clientRegistry.set(routeRef, client)

	log.V(1).Info("registered/updated A2A handler", "agent", agentRef)
	return nil
}

// debugHTTPClient returns nil in normal operation, letting the a2aclient SDK apply its
// default 3-minute request timeout. In debug mode it overrides the dial target so all
// A2A traffic is redirected to a fixed address (e.g. a local proxy).
func debugHTTPClient() *http.Client {
	debugAddr := env.KagentA2ADebugAddr.Get()
	if debugAddr == "" {
		return nil
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				var zeroDialer net.Dialer
				return zeroDialer.DialContext(ctx, network, debugAddr)
			},
		},
	}
}

func (a *A2ARegistrar) a2aRouteURL(agent v1alpha2.AgentObject) string {
	baseURL := a.a2aBaseURL
	if agent.GetWorkloadMode() == v1alpha2.WorkloadModeSandbox {
		baseURL = a.sandboxA2AURL
	}
	return baseURL + "/" + types.NamespacedName{Namespace: agent.GetNamespace(), Name: agent.GetName()}.String() + "/"
}

func a2aRouteKey(agent v1alpha2.AgentObject) string {
	return a2aRoutePath(agent)
}

func a2aRoutePath(agent v1alpha2.AgentObject) string {
	agentRef := types.NamespacedName{Namespace: agent.GetNamespace(), Name: agent.GetName()}
	return routeKey(agent.GetWorkloadMode() == v1alpha2.WorkloadModeSandbox, agentRef.Namespace, agentRef.Name)
}

// cloneInterfacesWithURL clones the interfaces and sets the URL to the given value.
func cloneInterfacesWithURL(interfaces []*a2atype.AgentInterface, url string) []*a2atype.AgentInterface {
	if len(interfaces) == 0 {
		return []*a2atype.AgentInterface{
			{
				URL:             url,
				ProtocolBinding: a2atype.TransportProtocolJSONRPC,
				ProtocolVersion: a2atype.Version,
			},
		}
	}
	result := make([]*a2atype.AgentInterface, 0, len(interfaces))
	for _, i := range interfaces {
		if i == nil {
			continue
		}
		copied := *i
		copied.URL = url
		if copied.ProtocolVersion == "" {
			copied.ProtocolVersion = a2atype.Version
		}
		result = append(result, &copied)
	}
	return result
}

// filterInterfacesByVersion filters the interfaces to only include the ones that match the given version.
// Currently, this is used to select the A2A 0.3 interface for managed agents.
func filterInterfacesByVersion(interfaces []*a2atype.AgentInterface, version a2atype.ProtocolVersion) []*a2atype.AgentInterface {
	filtered := make([]*a2atype.AgentInterface, 0, len(interfaces))
	for _, i := range interfaces {
		if i == nil {
			continue
		}
		if i.ProtocolVersion == version {
			filtered = append(filtered, i)
		}
	}
	if len(filtered) > 0 {
		return filtered
	}
	return interfaces
}
