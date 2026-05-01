package a2a

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agent_translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	common "github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
)

type A2ARegistrar struct {
	cache          crcache.Cache
	handlerMux     A2AHandlerMux
	a2aBaseURL     string
	sandboxA2AURL  string
	authenticator  auth.AuthProvider
	a2aBaseOptions []a2aclient.Option
}

var _ manager.Runnable = (*A2ARegistrar)(nil)

func NewA2ARegistrar(
	cache crcache.Cache,
	mux A2AHandlerMux,
	a2aBaseUrl string,
	sandboxA2ABaseURL string,
	authenticator auth.AuthProvider,
	streamingMaxBuf int,
	streamingInitialBuf int,
	streamingTimeout time.Duration,
) *A2ARegistrar {
	reg := &A2ARegistrar{
		cache:         cache,
		handlerMux:    mux,
		a2aBaseURL:    a2aBaseUrl,
		sandboxA2AURL: sandboxA2ABaseURL,
		authenticator: authenticator,
		a2aBaseOptions: []a2aclient.Option{
			a2aclient.WithTimeout(streamingTimeout),
			a2aclient.WithBuffer(streamingInitialBuf, streamingMaxBuf),
			debugOpt(),
		},
	}

	return reg
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
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			oldAgent, ok1 := informerAgentObject(oldObj)
			newAgent, ok2 := informerAgentObject(newObj)
			if !ok1 || !ok2 {
				return
			}
			if oldAgent.GetGeneration() != newAgent.GetGeneration() || !sameAgentSpec(oldAgent, newAgent) {
				if err := a.upsertAgentHandler(ctx, newAgent, log); err != nil {
					log.Error(err, "failed to upsert A2A handler", "agent", common.GetObjectRef(newAgent))
				}
			}
		},
		DeleteFunc: func(obj any) {
			agent, ok := deletedInformerAgentObject(obj)
			if !ok {
				return
			}
			ref := a.a2aRouteKey(agent)
			a.handlerMux.RemoveAgentHandler(ref)
			log.V(1).Info("removed A2A handler", "agent", ref)
		},
	}); err != nil {
		return fmt.Errorf("failed to add informer event handler for %T: %w", prototype, err)
	}

	return nil
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

	client, err := a2aclient.NewA2AClient(
		card.URL,
		append(
			a.a2aBaseOptions,
			a2aclient.WithHTTPReqHandler(
				&traceInjectHandler{
					next: authimpl.A2ARequestHandler(
						a.authenticator,
						agentRef,
					),
				},
			),
		)...,
	)
	if err != nil {
		return fmt.Errorf("create A2A client for %s: %w", agentRef, err)
	}

	cardCopy := *card
	cardCopy.URL = a.a2aRouteURL(agent)

	key := a.a2aRouteKey(agent)
	if err := a.handlerMux.SetAgentHandler(key, client, cardCopy, newA2ATracingMiddleware(agentRef, provider)); err != nil {
		return fmt.Errorf("set handler for %s: %w", agentRef, err)
	}

	log.V(1).Info("registered/updated A2A handler", "agent", agentRef)
	return nil
}

func debugOpt() a2aclient.Option {
	debugAddr := env.KagentA2ADebugAddr.Get()
	if debugAddr != "" {
		client := new(http.Client)
		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				var zeroDialer net.Dialer
				return zeroDialer.DialContext(ctx, network, debugAddr)
			},
		}
		return a2aclient.WithHTTPClient(client)
	} else {
		return func(*a2aclient.A2AClient) {}
	}
}

func (a *A2ARegistrar) a2aRouteURL(agent v1alpha2.AgentObject) string {
	baseURL := a.a2aBaseURL
	if agent.GetWorkloadMode() == v1alpha2.WorkloadModeSandbox {
		baseURL = a.sandboxA2AURL
	}
	return baseURL + "/" + types.NamespacedName{Namespace: agent.GetNamespace(), Name: agent.GetName()}.String() + "/"
}

func (a *A2ARegistrar) a2aRouteKey(agent v1alpha2.AgentObject) string {
	return routeKey(agent.GetWorkloadMode() == v1alpha2.WorkloadModeSandbox, agent.GetNamespace(), agent.GetName())
}
