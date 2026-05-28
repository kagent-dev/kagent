package a2a

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2aclient "github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
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
}

var _ A2AHandlerMux = &handlerMux{}

type middleware interface {
	Wrap(next http.Handler) http.Handler
}

func NewA2AHttpMux(agentPathPrefix, sandboxPathPrefix string, authenticator auth.AuthProvider) *handlerMux {
	return &handlerMux{
		handlers:          make(map[string]http.Handler),
		agentPathPrefix:   agentPathPrefix,
		sandboxPathPrefix: sandboxPathPrefix,
		authenticator:     authenticator,
	}
}

func (a *handlerMux) SetAgentHandler(
	agentRef string,
	client *a2aclient.Client,
	card a2atype.AgentCard,
	tracing middleware,
) error {
	requestHandler := NewPassthroughRequestHandler(client, &card)
	legacyJSONRPCHandler := a2av0.NewJSONRPCHandler(requestHandler)
	v1JSONRPCHandler := a2asrv.NewJSONRPCHandler(requestHandler)
	cardHandler := a2asrv.NewAgentCardHandler(a2av0.NewStaticAgentCardProducer(&card))
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
		switch wireVersion {
		case common.A2AWireVersionLegacy:
			legacyJSONRPCHandler.ServeHTTP(w, r)
		case common.A2AWireVersionV1:
			v1JSONRPCHandler.ServeHTTP(w, r)
		default:
			http.Error(w, fmt.Sprintf("unknown negotiated A2A wire version %q", wireVersion), http.StatusBadRequest)
		}
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
