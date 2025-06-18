package handlers

import (
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type NamespaceResponse struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Status string            `json:"status"`
}

// NamespacesHandler handles namespace-related requests
type NamespacesHandler struct {
	*Base
}

// NewNamespacesHandler creates a new NamespacesHandler
func NewNamespacesHandler(base *Base) *NamespacesHandler {
	return &NamespacesHandler{Base: base}
}

// HandleListNamespaces returns a list of all namespaces
func (h *NamespacesHandler) HandleListNamespaces(w ErrorResponseWriter, r *http.Request) {
	logger := log.FromContext(r.Context())

	namespaceList := &corev1.NamespaceList{}
	if err := h.KubeClient.List(r.Context(), namespaceList); err != nil {
		logger.Error(err, "Failed to list namespaces")
		http.Error(w, "Failed to list namespaces", http.StatusInternalServerError)
		return
	}

	var namespaces []NamespaceResponse
	for _, ns := range namespaceList.Items {
		namespaces = append(namespaces, NamespaceResponse{
			Name:   ns.Name,
			Labels: ns.Labels,
			Status: string(ns.Status.Phase),
		})
	}

	RespondWithJSON(w, http.StatusOK, namespaces)
}
