package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
)

type MemoryResponse struct {
	Ref             string                 `json:"ref"`
	ProviderName    string                 `json:"providerName"`
	APIKeySecretRef string                 `json:"apiKeySecretRef"`
	APIKeySecretKey string                 `json:"apiKeySecretKey"`
	MemoryParams    map[string]interface{} `json:"memoryParams"`
}

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

	memoryList := &v1alpha1.MemoryList{}
	if err := h.KubeClient.List(r.Context(), memoryList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list Memories", err))
		return
	}

	memoryResponses := make([]MemoryResponse, len(memoryList.Items))
	for i, memory := range memoryList.Items {
		memoryRef := common.GetObjectRef(&memory)
		log.V(1).Info("Processing Memory", "memoryRef", memoryRef)

		memoryParams := make(map[string]interface{})
		if memory.Spec.Pinecone != nil {
			FlattenStructToMap(memory.Spec.Pinecone, memoryParams)
		}

		memoryResponses[i] = MemoryResponse{
			Ref:             memoryRef,
			ProviderName:    string(memory.Spec.Provider),
			APIKeySecretRef: memory.Spec.APIKeySecretRef,
			APIKeySecretKey: memory.Spec.APIKeySecretKey,
			MemoryParams:    memoryParams,
		}
	}

	log.Info("Successfully listed Memories", "count", len(memoryResponses))
	RespondWithJSON(w, http.StatusOK, memoryResponses)
}

type CreateMemoryRequest struct {
	Ref            string                   `json:"ref"`
	Provider       Provider                 `json:"provider"`
	APIKey         string                   `json:"apiKey"`
	PineconeParams *v1alpha1.PineconeConfig `json:"pinecone,omitempty"`
}

// HandleCreateMemory handles POST /api/memories/ requests
func (h *MemoryHandler) HandleCreateMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "create")
	log.Info("Received request to create Memory")

	var req CreateMemoryRequest
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

	log.V(1).Info("Checking if Memory already exists")
	existingMemory := &v1alpha1.Memory{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		existingMemory,
		memoryRef.Name,
		memoryRef.Namespace,
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
	RespondWithJSON(w, http.StatusCreated, memory)
}

// HandleDeleteMemory handles DELETE /api/memories/{namespace}/{memoryName} requests
func (h *MemoryHandler) HandleDeleteMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "delete")
	log.Info("Received request to delete Memory")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	memoryName, err := GetPathParam(r, "memoryName")
	if err != nil {
		log.Error(err, "Failed to get memoryName from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get memoryName from path", err))
		return
	}

	log = log.WithValues(
		"memoryNamespace", namespace,
		"memoryName", memoryName,
	)

	log.V(1).Info("Checking if Memory exists")
	existingMemory := &v1alpha1.Memory{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		existingMemory,
		memoryName,
		namespace,
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
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Memory deleted successfully"})
}

// HandleGetMemory handles GET /api/memories/{namespace}/{memoryName} requests
func (h *MemoryHandler) HandleGetMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "get")
	log.Info("Received request to get Memory")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	memoryName, err := GetPathParam(r, "memoryName")
	if err != nil {
		log.Error(err, "Failed to get configName from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get configName from path", err))
		return
	}

	log = log.WithValues(
		"memoryNamespace", namespace,
		"memoryName", memoryName,
	)

	log.V(1).Info("Checking if Memory already exists")
	memory := &v1alpha1.Memory{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		memory,
		memoryName,
		namespace,
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

	memoryResponse := MemoryResponse{
		Ref:             common.GetObjectRef(memory),
		ProviderName:    string(memory.Spec.Provider),
		APIKeySecretRef: apiKeySecretRef.String(),
		APIKeySecretKey: memory.Spec.APIKeySecretKey,
		MemoryParams:    memoryParams,
	}

	log.Info("Memory retrieved successfully")
	RespondWithJSON(w, http.StatusOK, memoryResponse)
}

type UpdateMemoryRequest struct {
	PineconeParams *v1alpha1.PineconeConfig `json:"pinecone,omitempty"`
}

// HandleUpdateMemory handles PUT /api/memories/{namespace}/{memoryName} requests
func (h *MemoryHandler) HandleUpdateMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "update")
	log.Info("Received request to update Memory")

	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		log.Error(err, "Failed to get namespace from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}

	memoryName, err := GetPathParam(r, "memoryName")
	if err != nil {
		log.Error(err, "Failed to get config name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get config name from path", err))
		return
	}

	log = log.WithValues(
		"memoryNamespace", namespace,
		"memoryName", memoryName,
	)

	var req UpdateMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	existingMemory := &v1alpha1.Memory{}
	err = common.GetObject(
		r.Context(),
		h.KubeClient,
		existingMemory,
		memoryName,
		namespace,
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
	RespondWithJSON(w, http.StatusOK, existingMemory)
}
