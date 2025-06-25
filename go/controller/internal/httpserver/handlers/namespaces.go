package handlers

import (
	"net/http"

	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type NamespaceResponse struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// NamespacesHandler handles namespace-related requests
type NamespacesHandler struct {
	*Base
	// List of namespaces being watched, empty means watch all. Used for listing namespaces.
	// Can be moved to the base handler if any other handlers need it
	WatchedNamespaces []string
}

// NewNamespacesHandler creates a new NamespacesHandler
func NewNamespacesHandler(base *Base, watchedNamespaces []string) *NamespacesHandler {
	return &NamespacesHandler{
		Base:              base,
		WatchedNamespaces: watchedNamespaces,
	}
}

// HandleListNamespaces returns a list of namespaces based on the watch configuration
func (h *NamespacesHandler) HandleListNamespaces(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("namespaces-handler").WithValues("operation", "list")

	// If no watched namespaces are configured, list all namespaces in the cluster
	if len(h.WatchedNamespaces) == 0 {
		log.Info("Listing all namespaces (no watch filter configured)")
		namespaceList := &corev1.NamespaceList{}
		if err := h.KubeClient.List(r.Context(), namespaceList); err != nil {
			log.Error(err, "Failed to list namespaces")
			w.RespondWithError(errors.NewInternalServerError("Failed to list namespaces", err))
			return
		}

		var namespaces []NamespaceResponse
		for _, ns := range namespaceList.Items {
			namespaces = append(namespaces, NamespaceResponse{
				Name:   ns.Name,
				Status: string(ns.Status.Phase),
			})
		}

		RespondWithJSON(w, http.StatusOK, namespaces)
		return
	}

	// Filter to only show watched namespaces that exist in the cluster
	log.Info("Listing watched namespaces only", "watchedNamespaces", h.WatchedNamespaces)
	var namespaces []NamespaceResponse

	for _, watchedNS := range h.WatchedNamespaces {
		namespace := &corev1.Namespace{}
		if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Name: watchedNS}, namespace); err != nil {
			if client.IgnoreNotFound(err) != nil {
				log.Error(err, "Failed to get namespace", "namespace", watchedNS)
				continue // Skip this namespace
			}
			log.Info("Watched namespace not found", "namespace", watchedNS)
			continue
		}

		namespaces = append(namespaces, NamespaceResponse{
			Name:   namespace.Name,
			Status: string(namespace.Status.Phase),
		})
	}

	RespondWithJSON(w, http.StatusOK, namespaces)
}
