package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Registry is what an embedder may add to the core /mcp server. It exposes no
// middleware, so an extension cannot see or rewrite core's own tool calls.
type Registry interface {
	// AddTool needs a name, a handler and an input schema of type object.
	AddTool(tool *mcp.Tool, handler mcp.ToolHandler) error
	// AddResource needs a URI and a handler.
	AddResource(resource *mcp.Resource, handler mcp.ResourceHandler) error
	// AddResourceTemplate needs a URI template and a handler.
	AddResourceTemplate(template *mcp.ResourceTemplate, handler mcp.ResourceHandler) error
}

// applyExtensions runs extend against a registry that refuses any name or URI
// core or an earlier call registered, since the SDK would replace it silently.
// A refusal fails the result even if extend drops the error it was given.
func applyExtensions(server *mcp.Server, extend func(Registry) error) error {
	reg, err := newRegistry(server)
	if err != nil {
		return err
	}
	if err := extend(reg); err != nil {
		return fmt.Errorf("extend /mcp: %w", err)
	}
	if reg.err != nil {
		return fmt.Errorf("extend /mcp: %w", reg.err)
	}
	return nil
}

type registry struct {
	server                      *mcp.Server
	tools, resources, templates map[string]bool
	err                         error
}

// newRegistry records what the server already serves by listing it through an
// in-memory client, so a tool core adds later is protected without a second list.
func newRegistry(server *mcp.Server) (*registry, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("list core /mcp registrations: %w", err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "kagent-extensions"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, fmt.Errorf("list core /mcp registrations: %w", err)
	}
	defer clientSession.Close()

	reg := &registry{server: server, tools: map[string]bool{}, resources: map[string]bool{}, templates: map[string]bool{}}
	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list core tools: %w", err)
		}
		reg.tools[tool.Name] = true
	}
	for resource, err := range clientSession.Resources(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list core resources: %w", err)
		}
		reg.resources[resource.URI] = true
	}
	for template, err := range clientSession.ResourceTemplates(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list core resource templates: %w", err)
		}
		reg.templates[template.URITemplate] = true
	}
	return reg, nil
}

func (r *registry) AddTool(tool *mcp.Tool, handler mcp.ToolHandler) error {
	switch {
	case tool == nil || tool.Name == "" || handler == nil:
		return r.fail(errors.New("a tool needs a name and a handler"))
	case tool.InputSchema == nil:
		return r.fail(fmt.Errorf("tool %q needs an input schema", tool.Name))
	}
	return r.add("tool", tool.Name, r.tools, func() { r.server.AddTool(tool, handler) })
}

func (r *registry) AddResource(resource *mcp.Resource, handler mcp.ResourceHandler) error {
	if resource == nil || resource.URI == "" || handler == nil {
		return r.fail(errors.New("a resource needs a URI and a handler"))
	}
	return r.add("resource", resource.URI, r.resources, func() { r.server.AddResource(resource, handler) })
}

func (r *registry) AddResourceTemplate(template *mcp.ResourceTemplate, handler mcp.ResourceHandler) error {
	if template == nil || template.URITemplate == "" || handler == nil {
		return r.fail(errors.New("a resource template needs a URI template and a handler"))
	}
	return r.add("resource template", template.URITemplate, r.templates, func() { r.server.AddResourceTemplate(template, handler) })
}

// add claims key in taken, then registers. The SDK panics on input it rejects,
// such as a schema that is not an object; that becomes an error here.
func (r *registry) add(kind, key string, taken map[string]bool, register func()) (err error) {
	if taken[key] {
		return r.fail(fmt.Errorf("%s %q is already registered", kind, key))
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = r.fail(fmt.Errorf("%s %q: %v", kind, key, recovered))
		}
	}()
	register()
	taken[key] = true
	return nil
}

func (r *registry) fail(err error) error {
	r.err = errors.Join(r.err, err)
	return err
}
