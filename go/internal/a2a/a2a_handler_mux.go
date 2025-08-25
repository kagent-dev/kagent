package a2a

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/auth"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// A2AHandlerMux is an interface that defines methods for adding, getting, and removing agentic task handlers.
type A2AHandlerMux interface {
	SetAgentHandler(
		agentRef string,
		client *client.A2AClient,
		card server.AgentCard,
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
	client *client.A2AClient,
	card server.AgentCard,
) error {
	srv, err := server.NewA2AServer(card, NewPassthroughManager(client), server.WithMiddleWare(authimpl.NewA2AAuthenticator(a.authenticator)))
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

	// get the handler name from the first path segment
	path := strings.TrimPrefix(r.URL.Path, a.basePathPrefix)
	agentNamespace, remainingPath := popPath(path)
	if agentNamespace == "" {
		http.Error(w, "Agent namespace not provided", http.StatusBadRequest)
		return
	}
	agentName, _ := popPath(remainingPath)
	if agentName == "" {
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

// popPath separates the first element of a path from the rest.
// It returns the first path element and the remaining path.
// If the path is empty or only contains a separator, it returns empty strings.
func popPath(path string) (firstElement, remainingPath string) {
	// Remove leading slash if present
	path = strings.TrimPrefix(path, "/")

	// If path is empty after trimming, return empty strings
	if path == "" {
		return "", ""
	}

	// Find the position of the first separator
	pos := strings.Index(path, "/")

	// If no separator found, the first element is the entire path
	if pos == -1 {
		return path, ""
	}

	// Split the path at the first separator
	firstElement = path[:pos]
	remainingPath = path[pos+1:] // Skip the separator

	return firstElement, remainingPath
}
