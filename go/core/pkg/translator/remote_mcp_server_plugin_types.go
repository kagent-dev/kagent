package translator

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RemoteMCPServerOutputs collects the resources a RemoteMCPServerPlugin wants
// reconciled for a RemoteMCPServer. The reconciler applies these objects and
// prunes any previously-owned objects (of the plugin's GetOwnedResourceTypes)
// that the plugin no longer returns, keying ownership on the RemoteMCPServer.
type RemoteMCPServerOutputs struct {
	// Manifest is the set of objects the plugin wants to exist for this
	// RemoteMCPServer. The reconciler sets the controller reference to the
	// RemoteMCPServer on each, so the objects must live in the RMS's own
	// namespace and are garbage-collected when the RMS is deleted.
	Manifest []client.Object
}

// RemoteMCPServerPlugin is a reconcile-phase extension point that runs during
// the RemoteMCPServer reconcile. It lets an extension author resources for a
// RemoteMCPServer without standing up a second controller that watches the
// same RemoteMCPServer.
//
// It mirrors the agent-side TranslatorPlugin: the plugin appends desired
// objects to outputs.Manifest, the reconciler owns them to the RMS and
// reconciles them (apply + prune of its GetOwnedResourceTypes). A plugin error
// fails only the RemoteMCPServer reconcile (which is retried); it never affects
// agent translation. Registered plugins are invoked unconditionally — any
// feature gating is the plugin's decision.
type RemoteMCPServerPlugin interface {
	// ProcessRemoteMCPServer is called on each reconcile of server. The plugin
	// appends the objects it wants to exist to outputs.Manifest.
	ProcessRemoteMCPServer(ctx context.Context, server *v1alpha2.RemoteMCPServer, outputs *RemoteMCPServerOutputs) error
	// GetOwnedResourceTypes returns the object types this plugin may author, so
	// the reconciler can set up owner indexes and prune stale objects. Mirrors
	// TranslatorPlugin.GetOwnedResourceTypes.
	GetOwnedResourceTypes() []client.Object
}
