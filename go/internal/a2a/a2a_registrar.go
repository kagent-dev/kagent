package a2a

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agent_translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	crcache "sigs.k8s.io/controller-runtime/pkg/cache"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
)

type A2ARegistrar struct {
	cache          crcache.Cache
	translator     agent_translator.AdkApiTranslator
	handlerMux     A2AHandlerMux
	a2aBaseUrl     string
	authenticator  auth.AuthProvider
	a2aBaseOptions []a2aclient.Option
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
	reg := &A2ARegistrar{
		cache:         cache,
		translator:    translator,
		handlerMux:    mux,
		a2aBaseUrl:    a2aBaseUrl,
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

	client, err := a2aclient.NewA2AClient(
		card.URL,
		append(
			a.a2aBaseOptions,
			a2aclient.WithHTTPReqHandler(
				authimpl.A2ARequestHandler(
					a.authenticator,
					agentRef,
				),
			),
		)...,
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

func debugOpt() a2aclient.Option {
	debugAddr := os.Getenv("KAGENT_A2A_DEBUG_ADDR")
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
