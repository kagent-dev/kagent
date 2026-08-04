package a2a

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aext"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/gorilla/mux"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	common "github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// A2AHandlerMux is an interface that defines methods for adding, getting, and removing agentic task handlers.
type A2AHandlerMux interface {
	SetAgentHandler(
		agentRef string,
		client *a2aclient.Client,
		card a2atype.AgentCard,
		tracing middleware,
	) error
	RemoveAgentHandler(
		agentRef string,
	)
	http.Handler
}

type handlerMux struct {
	handlers          map[string]http.Handler
	lock              sync.RWMutex
	agentPathPrefix   string
	sandboxPathPrefix string
	authenticator     auth.AuthProvider
	taskStore         TaskStore
}

var _ A2AHandlerMux = &handlerMux{}

type middleware interface {
	Wrap(next http.Handler) http.Handler
}

func NewA2AHttpMux(agentPathPrefix, sandboxPathPrefix string, authenticator auth.AuthProvider, taskStore TaskStore) *handlerMux {
	return &handlerMux{
		handlers:          make(map[string]http.Handler),
		agentPathPrefix:   agentPathPrefix,
		sandboxPathPrefix: sandboxPathPrefix,
		authenticator:     authenticator,
		taskStore:         taskStore,
	}
}

// newTaskQueryHandler wraps the passthrough handler so ListTasks is served from
// kagent's task store, which is the source of truth for persisted tasks.
func newTaskQueryHandler(requestHandler a2asrv.RequestHandler, store TaskStore) a2asrv.RequestHandler {
	if store == nil {
		return requestHandler
	}
	return newStoreTaskQueryHandler(requestHandler, store)
}

// newProxyRequestHandler preserves negotiated A2A extension headers and
// extension metadata while forwarding a typed request through the controller.
// The matching client propagator is installed when the upstream client is
// constructed in A2ARegistrar.
func newProxyRequestHandler(client *a2aclient.Client, card *a2atype.AgentCard, store TaskStore) a2asrv.RequestHandler {
	delegate := newTaskQueryHandler(NewPassthroughRequestHandler(client, card), store)
	return &a2asrv.InterceptedHandler{
		Handler:      delegate,
		Interceptors: []a2asrv.CallInterceptor{a2aext.NewServerPropagator(nil)},
	}
}

func (a *handlerMux) SetAgentHandler(
	agentRef string,
	client *a2aclient.Client,
	card a2atype.AgentCard,
	tracing middleware,
) error {
	requestHandler := newProxyRequestHandler(client, &card, a.taskStore)
	jsonRPCHandler := a2asrv.NewJSONRPCHandler(requestHandler)
	cardHandler := a2asrv.NewStaticAgentCardHandler(&card)
	wellKnownPath := "/" + strings.TrimPrefix(a2asrv.WellKnownAgentCardPath, "/")

	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, wellKnownPath) {
			cardHandler.ServeHTTP(w, r)
			return
		}
		wireVersion, err := common.NegotiateA2AWireVersion(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if wireVersion != common.A2AWireVersionV1 {
			http.Error(w, "unsupported negotiated A2A wire version", http.StatusBadRequest)
			return
		}
		jsonRPCHandler.ServeHTTP(w, r)
	})
	middlewares := []middleware{authimpl.NewA2AAuthenticator(a.authenticator)}
	if tracing != nil {
		middlewares = append(middlewares, tracing)
	}
	for _, middleware := range slices.Backward(middlewares) {
		handler = middleware.Wrap(handler)
	}

	a.lock.Lock()
	defer a.lock.Unlock()

	a.handlers[agentRef] = handler

	return nil
}

func (a *handlerMux) RemoveAgentHandler(
	agentRef string,
) {
	a.lock.Lock()
	defer a.lock.Unlock()
	delete(a.handlers, agentRef)
}

func (a *handlerMux) getHandler(name string) (http.Handler, bool) {
	a.lock.RLock()
	defer a.lock.RUnlock()
	handler, ok := a.handlers[name]
	return handler, ok
}

func (a *handlerMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	// get the handler name from the first path segment
	agentNamespace, ok := vars["namespace"]
	if !ok || agentNamespace == "" {
		http.Error(w, "Agent namespace not provided", http.StatusBadRequest)
		return
	}
	agentName, ok := vars["name"]
	if !ok || agentName == "" {
		http.Error(w, "Agent name not provided", http.StatusBadRequest)
		return
	}

	handlerName := routeKey(a.isSandboxRoute(r), agentNamespace, agentName)

	// get the underlying handler
	handlerHandler, ok := a.getHandler(handlerName)
	if !ok {
		http.Error(
			w,
			fmt.Sprintf("Agent %s not found", handlerName),
			http.StatusNotFound,
		)
		return
	}

	handlerHandler.ServeHTTP(w, r)
}

func (a *handlerMux) isSandboxRoute(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, a.sandboxPathPrefix+"/") || r.URL.Path == a.sandboxPathPrefix
}

func routeKey(isSandbox bool, namespace, name string) string {
	if isSandbox {
		return common.ResourceRefString("sandboxes", common.ResourceRefString(namespace, name))
	}
	return common.ResourceRefString(namespace, name)
}
