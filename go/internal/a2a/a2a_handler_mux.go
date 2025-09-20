package a2a

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/server"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// A2AHandlerMux is an interface that defines methods for adding, getting, and removing agentic task handlers.
type A2AHandlerMux interface {
	SetAgentHandler(
		agentRef string,
		client *client.A2AClient,
		card server.AgentCard,
		agentType v1alpha2.AgentType,
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
	dbClient       database.Client
}

var _ A2AHandlerMux = &handlerMux{}

func NewA2AHttpMux(pathPrefix string, authenticator auth.AuthProvider, dbClient database.Client) *handlerMux {
	return &handlerMux{
		handlers:       make(map[string]http.Handler),
		basePathPrefix: pathPrefix,
		authenticator:  authenticator,
		dbClient:       dbClient,
	}
}

func (a *handlerMux) SetAgentHandler(
	agentRef string,
	client *client.A2AClient,
	card server.AgentCard,
	agentType v1alpha2.AgentType,
) error {
	var manager taskmanager.TaskManager
	if agentType == v1alpha2.AgentType_Remote {
		manager = NewRecordingManager(client, a.dbClient, agentRef)
	} else {
		manager = NewPassthroughManager(client)
	}

	srv, err := server.NewA2AServer(card, manager, server.WithMiddleWare(authimpl.NewA2AAuthenticator(a.authenticator)))
	if err != nil {
		return fmt.Errorf("failed to create A2A server: %w", err)
	}

	a.lock.Lock()
	defer a.lock.Unlock()

	a.handlers[agentRef] = srv.Handler()

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
