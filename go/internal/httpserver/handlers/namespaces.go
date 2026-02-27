package handlers

import (
	"net/http"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	corev1 "k8s.io/api/core/v1"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

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

		var namespaces []api.NamespaceResponse
		for _, ns := range namespaceList.Items {
			namespaces = append(namespaces, api.NamespaceResponse{
				Name:   ns.Name,
				Status: string(ns.Status.Phase),
			})
		}

		slices.SortStableFunc(namespaces, func(i, j api.NamespaceResponse) int {
			return strings.Compare(strings.ToLower(i.Name), strings.ToLower(j.Name))
		})

		data := api.NewResponse(namespaces, "Successfully listed namespaces", false)
		RespondWithJSON(w, http.StatusOK, data)
		return
	}

	// Filter to only show watched namespaces that exist in the cluster
	log.Info("Listing watched namespaces only", "watchedNamespaces", h.WatchedNamespaces)
	var namespaces []api.NamespaceResponse

	for _, watchedNS := range h.WatchedNamespaces {
		namespace := &corev1.Namespace{}
		if err := h.KubeClient.Get(r.Context(), ctrl_client.ObjectKey{Name: watchedNS}, namespace); err != nil {
			if ctrl_client.IgnoreNotFound(err) != nil {
				log.Error(err, "Failed to get namespace", "namespace", watchedNS)
				continue // Skip this namespace
			}
			log.Info("Watched namespace not found", "namespace", watchedNS)
			continue
		}

		namespaces = append(namespaces, api.NamespaceResponse{
			Name:   namespace.Name,
			Status: string(namespace.Status.Phase),
		})
	}

	slices.SortStableFunc(namespaces, func(i, j api.NamespaceResponse) int {
		return strings.Compare(strings.ToLower(i.Name), strings.ToLower(j.Name))
	})

	data := api.NewResponse(namespaces, "Successfully listed namespaces", false)
	RespondWithJSON(w, http.StatusOK, data)
}
