package a2a

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	common "github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"sigs.k8s.io/controller-runtime/pkg/client"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// A2AHandlerMux is an interface that defines methods for adding, getting, and removing agentic task handlers.
type A2AHandlerMux interface {
	SetAgentHandler(
		agentRef string,
		client *a2aclient.A2AClient,
		card server.AgentCard,
		tracing server.Middleware,
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
	authorizer        auth.Authorizer
	kubeClient        client.Client
}

var _ A2AHandlerMux = &handlerMux{}

func NewA2AHttpMux(agentPathPrefix, sandboxPathPrefix string, authenticator auth.AuthProvider, authorizer auth.Authorizer, kubeClient client.Client) *handlerMux {
	return &handlerMux{
		handlers:          make(map[string]http.Handler),
		agentPathPrefix:   agentPathPrefix,
		sandboxPathPrefix: sandboxPathPrefix,
		authenticator:     authenticator,
		authorizer:        authorizer,
		kubeClient:        kubeClient,
	}
}

func (a *handlerMux) SetAgentHandler(
	agentRef string,
	client *a2aclient.A2AClient,
	card server.AgentCard,
	tracing server.Middleware,
) error {
	middlewares := []server.Middleware{authimpl.NewA2AAuthenticator(a.authenticator)}
	if tracing != nil {
		middlewares = append(middlewares, tracing)
	}
	srv, err := server.NewA2AServer(card, NewPassthroughManager(client), server.WithMiddleWare(middlewares...))
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

	// Authorization check: verify user's groups can access this agent
	if a.authorizer != nil {
		session, sessionOk := auth.AuthSessionFrom(r.Context())
		if sessionOk {
			resource := auth.Resource{
				Type: "Agent",
				Name: agentNamespace + "/" + agentName,
			}
			if err := a.authorizer.Check(r.Context(), session.Principal(), auth.VerbGet, resource); err != nil {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
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
