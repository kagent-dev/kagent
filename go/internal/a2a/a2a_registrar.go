package a2a

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agent_translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/env"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type A2ARegistrar struct {
	cache            crcache.Cache
	translator       agent_translator.AdkApiTranslator
	handlerMux       A2AHandlerMux
	a2aBaseUrl       string
	authenticator    auth.AuthProvider
	streamingTimeout time.Duration
}

var _ manager.Runnable = (*A2ARegistrar)(nil)

func NewA2ARegistrar(
	cache crcache.Cache,
	translator agent_translator.AdkApiTranslator,
	mux A2AHandlerMux,
	a2aBaseUrl string,
	authenticator auth.AuthProvider,
	streamingMaxBuf int,
	streamingInitialBuf int,
	streamingTimeout time.Duration,
) *A2ARegistrar {
	return &A2ARegistrar{
		cache:            cache,
		translator:       translator,
		handlerMux:       mux,
		a2aBaseUrl:       a2aBaseUrl,
		authenticator:    authenticator,
		streamingTimeout: streamingTimeout,
	}
}

func (a *A2ARegistrar) NeedLeaderElection() bool {
	return false
}

func (a *A2ARegistrar) Start(ctx context.Context) error {
	log := ctrllog.FromContext(ctx).WithName("a2a-registrar")

	informer, err := a.cache.GetInformer(ctx, &v1alpha2.Agent{})
	if err != nil {
		return fmt.Errorf("failed to get cache informer: %w", err)
	}

	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if agent, ok := obj.(*v1alpha2.Agent); ok {
				if err := a.upsertAgentHandler(ctx, agent, log); err != nil {
					log.Error(err, "failed to upsert A2A handler", "agent", common.GetObjectRef(agent))
				}
			}
		},
		UpdateFunc: func(oldObj, newObj any) {
			oldAgent, ok1 := oldObj.(*v1alpha2.Agent)
			newAgent, ok2 := newObj.(*v1alpha2.Agent)
			if !ok1 || !ok2 {
				return
			}
			if oldAgent.Generation != newAgent.Generation || !reflect.DeepEqual(oldAgent.Spec, newAgent.Spec) {
				if err := a.upsertAgentHandler(ctx, newAgent, log); err != nil {
					log.Error(err, "failed to upsert A2A handler", "agent", common.GetObjectRef(newAgent))
				}
			}
		},
		DeleteFunc: func(obj any) {
			agent, ok := obj.(*v1alpha2.Agent)
			if !ok {
				if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
					if a2, ok := tombstone.Obj.(*v1alpha2.Agent); ok {
						agent = a2
					}
				}
			}
			if agent == nil {
				return
			}
			ref := common.GetObjectRef(agent)
			a.handlerMux.RemoveAgentHandler(ref)
			log.V(1).Info("removed A2A handler", "agent", ref)
		},
	}); err != nil {
		return fmt.Errorf("failed to add informer event handler: %w", err)
	}

	if ok := a.cache.WaitForCacheSync(ctx); !ok {
		return fmt.Errorf("cache sync failed")
	}

	<-ctx.Done()
	return nil
}

func (a *A2ARegistrar) upsertAgentHandler(ctx context.Context, agent *v1alpha2.Agent, log logr.Logger) error {
	agentRef := types.NamespacedName{Namespace: agent.GetNamespace(), Name: agent.GetName()}
	card := agent_translator.GetA2AAgentCard(agent)

	httpClient := &http.Client{
		Timeout:   a.streamingTimeout,
		Transport: a.buildTransport(agentRef),
	}

	endpoints := []a2a.AgentInterface{
		{Transport: a2a.TransportProtocolJSONRPC, URL: card.URL},
	}

	client, err := a2aclient.NewFromEndpoints(
		ctx,
		endpoints,
		a2aclient.WithDefaultsDisabled(),
		a2aclient.WithJSONRPCTransport(httpClient),
	)
	if err != nil {
		return fmt.Errorf("create A2A client for %s: %w", agentRef, err)
	}

	cardCopy := *card
	cardCopy.URL = fmt.Sprintf("%s/%s/", a.a2aBaseUrl, agentRef)

	if err := a.handlerMux.SetAgentHandler(agentRef.String(), client, cardCopy); err != nil {
		return fmt.Errorf("set handler for %s: %w", agentRef, err)
	}

	log.V(1).Info("registered/updated A2A handler", "agent", agentRef)
	return nil
}

func (a *A2ARegistrar) buildTransport(agentRef types.NamespacedName) http.RoundTripper {
	var baseTransport http.RoundTripper

	debugAddr := env.KagentA2ADebugAddr.Get()
	if debugAddr != "" {
		baseTransport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				var zeroDialer net.Dialer
				return zeroDialer.DialContext(ctx, network, debugAddr)
			},
		}
	}

	return &authimpl.A2AAuthRoundTripper{
		Base:         baseTransport,
		AuthProvider: a.authenticator,
		UpstreamPrincipal: auth.Principal{
			Agent: auth.Agent{
				ID: agentRef.String(),
			},
		},
	}
}
