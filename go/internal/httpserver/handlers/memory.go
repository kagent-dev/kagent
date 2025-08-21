package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/kagent/go/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

// MemoryHandler handles Memory requests
type MemoryHandler struct {
	*Base
}

// NewMemoryHandler creates a new MemoryHandler
func NewMemoryHandler(base *Base) *MemoryHandler {
	return &MemoryHandler{Base: base}
}

// HandleListMemories handles GET /api/memories/ requests
func (h *MemoryHandler) HandleListMemories(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "list-memories")
	log.Info("Listing Memories")

	if err := Check(h.Authorizer, r, auth.Resource{Type: "Memory"}); err != nil {
		w.RespondWithError(err)
		return
	}
	memoryList := &v1alpha1.MemoryList{}
	if err := h.KubeClient.List(r.Context(), memoryList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list Memories", err))
		return
	}

	memoryResponses := make([]api.MemoryResponse, len(memoryList.Items))
	for i, memory := range memoryList.Items {
		memoryRef := common.GetObjectRef(&memory)
		log.V(1).Info("Processing Memory", "memoryRef", memoryRef)

		memoryParams := make(map[string]interface{})
		if memory.Spec.Pinecone != nil {
			FlattenStructToMap(memory.Spec.Pinecone, memoryParams)
		}

		memoryResponses[i] = api.MemoryResponse{
			Ref:             memoryRef,
			ProviderName:    string(memory.Spec.Provider),
			APIKeySecretRef: memory.Spec.APIKeySecretRef,
			APIKeySecretKey: memory.Spec.APIKeySecretKey,
			MemoryParams:    memoryParams,
		}
	}

	log.Info("Successfully listed Memories", "count", len(memoryResponses))
	data := api.NewResponse(memoryResponses, "Successfully listed Memories", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleCreateMemory handles POST /api/memories/ requests
func (h *MemoryHandler) HandleCreateMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "create")
	log.Info("Received request to create Memory")

	var req api.CreateMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	memoryRef, err := common.ParseRefString(req.Ref, common.GetResourceNamespace())
	if err != nil {
		log.Error(err, "Failed to parse Ref")
		w.RespondWithError(errors.NewBadRequestError("Invalid Ref", err))
		return
	}
	if !strings.Contains(req.Ref, "/") {
		log.V(4).Info("Namespace not provided in request. Creating in controller installation namespace",
			"defaultNamespace", memoryRef.Namespace)
	}

	log = log.WithValues(
		"memoryNamespace", memoryRef.Namespace,
		"memoryName", memoryRef.Name,
		"provider", req.Provider.Type,
	)
	if err := Check(h.Authorizer, r, auth.Resource{Type: "Memory", Name: memoryRef.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	log.V(1).Info("Checking if Memory already exists")
	existingMemory := &v1alpha1.Memory{}
	err = h.KubeClient.Get(
		r.Context(),
		memoryRef,
		existingMemory,
	)
	if err == nil {
		log.Info("Memory already exists")
		w.RespondWithError(errors.NewConflictError("Memory already exists", nil))
		return
	} else if !k8serrors.IsNotFound(err) {
		log.Error(err, "Failed to check if Memory exists")
		w.RespondWithError(errors.NewInternalServerError("Failed to check if Memory exists", err))
		return
	}

	providerTypeEnum := v1alpha1.MemoryProvider(req.Provider.Type)
	memorySpec := v1alpha1.MemorySpec{
		Provider:        providerTypeEnum,
		APIKeySecretRef: memoryRef.String(),
		APIKeySecretKey: fmt.Sprintf("%s_API_KEY", strings.ToUpper(req.Provider.Type)),
	}

	if providerTypeEnum == v1alpha1.Pinecone {
		memorySpec.Pinecone = req.PineconeParams
	}

	memory := &v1alpha1.Memory{
		ObjectMeta: metav1.ObjectMeta{
			Name:      memoryRef.Name,
			Namespace: memoryRef.Namespace,
		},
		Spec: memorySpec,
	}

	if err := h.KubeClient.Create(r.Context(), memory); err != nil {
		log.Error(err, "Failed to create Memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to create Memory", err))
		return
	}
	log.V(1).Info("Successfully created Memory")

	err = createSecretWithOwnerReference(
		r.Context(),
		h.KubeClient,
		map[string]string{memorySpec.APIKeySecretKey: req.APIKey},
		memory,
	)
	if err != nil {
		log.Error(err, "Failed to create Memory API key secret")
	} else {
		log.V(1).Info("Successfully created Memory API key secret with OwnerReference")
	}

	log.Info("Memory created successfully")
	data := api.NewResponse(memory, "Successfully created Memory", false)
	RespondWithJSON(w, http.StatusCreated, data)
}

// HandleDeleteMemory handles DELETE /api/memories/{namespace}/{name} requests
func (h *MemoryHandler) HandleDeleteMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "delete")
	log.Info("Received request to delete Memory")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	memoryName, err := GetPathParam(r, "name")
	if err != nil {
		log.Error(err, "Failed to get name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	log = log.WithValues(
		"memoryNamespace", namespace,
		"memoryName", memoryName,
	)
	if err := Check(h.Authorizer, r, auth.Resource{Type: "Memory", Name: types.NamespacedName{Namespace: namespace, Name: memoryName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	log.V(1).Info("Checking if Memory exists")
	existingMemory := &v1alpha1.Memory{}
	err = h.KubeClient.Get(
		r.Context(),
		client.ObjectKey{
			Namespace: namespace,
			Name:      memoryName,
		},
		existingMemory,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Memory not found")
			w.RespondWithError(errors.NewNotFoundError("Memory not found", nil))
			return
		}
		log.Error(err, "Failed to get Memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to get Memory", err))
		return
	}

	log.Info("Deleting Memory")
	if err := h.KubeClient.Delete(r.Context(), existingMemory); err != nil {
		log.Error(err, "Failed to delete Memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to delete Memory", err))
		return
	}

	log.Info("Memory deleted successfully")
	data := api.NewResponse(struct{}{}, "Memory deleted successfully", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleGetMemory handles GET /api/memories/{namespace}/{name} requests
func (h *MemoryHandler) HandleGetMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "get")
	log.Info("Received request to get Memory")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	memoryName, err := GetPathParam(r, "name")
	if err != nil {
		log.Error(err, "Failed to get name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	log = log.WithValues(
		"memoryNamespace", namespace,
		"memoryName", memoryName,
	)
	if err := Check(h.Authorizer, r, auth.Resource{Type: "Memory", Name: types.NamespacedName{Namespace: namespace, Name: memoryName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	log.V(1).Info("Checking if Memory already exists")
	memory := &v1alpha1.Memory{}
	err = h.KubeClient.Get(
		r.Context(),
		client.ObjectKey{
			Namespace: namespace,
			Name:      memoryName,
		},
		memory,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Memory not found")
			w.RespondWithError(errors.NewNotFoundError("Memory not found", nil))
			return
		}
		log.Error(err, "Failed to get Memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to get Memory", err))
		return
	}

	memoryParams := make(map[string]interface{})
	if memory.Spec.Pinecone != nil {
		FlattenStructToMap(memory.Spec.Pinecone, memoryParams)
	}

	apiKeySecretRef, err := common.ParseRefString(memory.Spec.APIKeySecretRef, memory.Namespace)
	if err != nil {
		log.Error(err, "Failed to parse APIKeySecretRef")
		w.RespondWithError(errors.NewBadRequestError("Failed to parse APIKeySecretRef", err))
		return
	}

	memoryResponse := api.MemoryResponse{
		Ref:             common.GetObjectRef(memory),
		ProviderName:    string(memory.Spec.Provider),
		APIKeySecretRef: apiKeySecretRef.String(),
		APIKeySecretKey: memory.Spec.APIKeySecretKey,
		MemoryParams:    memoryParams,
	}

	log.Info("Memory retrieved successfully")
	data := api.NewResponse(memoryResponse, "Successfully retrieved Memory", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleUpdateMemory handles PUT /api/memories/{namespace}/{name} requests
func (h *MemoryHandler) HandleUpdateMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "update")
	log.Info("Received request to update Memory")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	memoryName, err := GetPathParam(r, "name")
	if err != nil {
		log.Error(err, "Failed to get name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	log = log.WithValues(
		"memoryNamespace", namespace,
		"memoryName", memoryName,
	)
	if err := Check(h.Authorizer, r, auth.Resource{Type: "Memory", Name: types.NamespacedName{Namespace: namespace, Name: memoryName}.String()}); err != nil {
		w.RespondWithError(err)
		return
	}

	var req api.UpdateMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	existingMemory := &v1alpha1.Memory{}
	err = h.KubeClient.Get(
		r.Context(),
		client.ObjectKey{
			Namespace: namespace,
			Name:      memoryName,
		},
		existingMemory,
	)
	if err != nil {
		log.Error(err, "Failed to get Memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to get Memory", err))
		return
	}

	if req.PineconeParams != nil {
		existingMemory.Spec.Pinecone = req.PineconeParams
	}

	if err := h.KubeClient.Update(r.Context(), existingMemory); err != nil {
		log.Error(err, "Failed to update Memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to update Memory", err))
		return
	}

	log.Info("Memory updated successfully")
	data := api.NewResponse(existingMemory, "Successfully updated Memory", false)
	RespondWithJSON(w, http.StatusOK, data)
}
