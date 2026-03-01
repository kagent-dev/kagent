package a2a

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/gorilla/mux"
	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
)

// A2AHandlerMux is an interface that defines methods for adding, getting, and removing agentic task handlers.
type A2AHandlerMux interface {
	SetAgentHandler(
		agentRef string,
		client *a2aclient.Client,
		card a2a.AgentCard,
	) error
	RemoveAgentHandler(
		agentRef string,
	)
	http.Handler
}

type handlerMux struct {
	handlers       map[string]http.Handler
	lock           sync.RWMutex
	basePathPrefix string
	authenticator  auth.AuthProvider
}

var _ A2AHandlerMux = &handlerMux{}

func NewA2AHttpMux(pathPrefix string, authenticator auth.AuthProvider) *handlerMux {
	return &handlerMux{
		handlers:       make(map[string]http.Handler),
		basePathPrefix: pathPrefix,
		authenticator:  authenticator,
	}
}

func (a *handlerMux) SetAgentHandler(
	agentRef string,
	client *a2aclient.Client,
	card a2a.AgentCard,
) error {
	passthroughHandler := NewPassthroughHandler(client)
	jsonrpcHandler := a2asrv.NewJSONRPCHandler(passthroughHandler)
	authnMiddleware := authimpl.NewA2AAuthenticator(a.authenticator)
	wrappedHandler := authnMiddleware.Wrap(jsonrpcHandler)

	a.lock.Lock()
	defer a.lock.Unlock()

	a.handlers[agentRef] = wrappedHandler

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

	handlerName := common.ResourceRefString(agentNamespace, agentName)

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
