package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type MemoryResponse struct {
	Name            string                 `json:"name"`
	Namespace       string                 `json:"namespace"`
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
		memoryParams := make(map[string]interface{})
		if memory.Spec.Pinecone != nil {
			FlattenStructToMap(memory.Spec.Pinecone, memoryParams)
		}
		memoryResponses[i] = MemoryResponse{
			Name:            memory.Name,
			Namespace:       memory.Namespace,
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
	Name           string                   `json:"name"`
	Namespace      string                   `json:"namespace"`
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

	if req.Namespace == "" {
		req.Namespace = common.GetResourceNamespace()
		log.V(1).Info("Namespace not provided in request. Creating in", req.Namespace, "namespace")
	}

	log = log.WithValues(
		"namespace", req.Namespace,
		"memoryName", req.Name,
		"provider", req.Provider.Type,
	)

	log.V(1).Info("Checking if Memory already exists")
	existingMemory := &v1alpha1.Memory{}
	err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      req.Name,
		Namespace: req.Namespace,
	}, existingMemory)
	if err == nil {
		log.Info("Memory already exists")
		w.RespondWithError(errors.NewConflictError("Memory already exists", nil))
		return
	} else if !k8serrors.IsNotFound(err) {
		log.Error(err, "Failed to check if Memory exists")
		w.RespondWithError(errors.NewInternalServerError("Failed to check if Memory exists", err))
		return
	}

	// TODO(sbx0r): Handle situation where the secret already exist

	providerTypeEnum := v1alpha1.MemoryProvider(req.Provider.Type)
	memorySpec := v1alpha1.MemorySpec{
		Provider:        providerTypeEnum,
		APIKeySecretRef: req.Namespace + "/" + req.Name,
		APIKeySecretKey: fmt.Sprintf("%s_API_KEY", strings.ToUpper(req.Provider.Type)),
	}

	if providerTypeEnum == v1alpha1.Pinecone {
		memorySpec.Pinecone = req.PineconeParams
	}

	apiKey := req.APIKey
	_, err = CreateSecret(
		h.KubeClient,
		req.Name,
		req.Namespace,
		map[string]string{memorySpec.APIKeySecretKey: apiKey},
	)
	if err != nil {
		log.Error(err, "Failed to create Memory API key secret")
		log.Error(err, "namespace", req.Namespace, "name", req.Name)
		w.RespondWithError(errors.NewInternalServerError("Failed to create Memory API key secret", err))
		return
	}
	log.V(1).Info("Successfully created Memory API key secret")
	memory := &v1alpha1.Memory{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
		},
		Spec: memorySpec,
	}

	if err := h.KubeClient.Create(r.Context(), memory); err != nil {
		log.Error(err, "Failed to create Memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to create Memory", err))
		return
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
		log.Error(err, "Failed to get config name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get memoryName from path", err))
		return
	}

	log = log.WithValues(
		"namespace", namespace,
		"memoryName", memoryName,
	)

	log.V(1).Info("Checking if Memory exists")
	existingMemory := &v1alpha1.Memory{}
	err = h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      memoryName,
		Namespace: namespace,
	}, existingMemory)
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
		"namespace", namespace,
		"memoryName", memoryName,
	)

	log.V(1).Info("Checking if Memory already exists")
	memory := &v1alpha1.Memory{}
	err = h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      memoryName,
		Namespace: namespace,
	}, memory)
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
	memoryResponse := MemoryResponse{
		Name:            memory.Name,
		Namespace:       memory.Namespace,
		ProviderName:    string(memory.Spec.Provider),
		APIKeySecretRef: memory.Spec.APIKeySecretRef,
		APIKeySecretKey: memory.Spec.APIKeySecretKey,
		MemoryParams:    memoryParams,
	}

	log.Info("Memory retrieved successfully")
	RespondWithJSON(w, http.StatusOK, memoryResponse)
}

type UpdateMemoryRequest struct {
	Name           string                   `json:"name"`
	Namespace      string                   `json:"namespace,omitempty"`
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
		"namespace", namespace,
		"memoryName", memoryName,
	)

	var req UpdateMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	existingMemory := &v1alpha1.Memory{}
	err = h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      memoryName,
		Namespace: namespace,
	}, existingMemory)
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
