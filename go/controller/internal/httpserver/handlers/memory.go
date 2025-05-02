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

// MemoryHandler handles memory requests
type MemoryHandler struct {
	*Base
}

// NewMemoryHandler creates a new MemoryHandler
func NewMemoryHandler(base *Base) *MemoryHandler {
	return &MemoryHandler{Base: base}
}

func (h *MemoryHandler) HandleListMemories(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "list-memories")

	log.Info("Listing memories")

	memoryList := &v1alpha1.MemoryList{}
	if err := h.KubeClient.List(r.Context(), memoryList); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list memories", err))
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

	RespondWithJSON(w, http.StatusOK, memoryResponses)
}

type CreateMemoryRequest struct {
	Name           string                   `json:"name"`
	Provider       Provider                 `json:"provider"`
	APIKey         string                   `json:"apiKey"`
	PineconeParams *v1alpha1.PineconeConfig `json:"pinecone,omitempty"`
}

func (h *MemoryHandler) HandleCreateMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "create")

	var req CreateMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error(err, "Failed to decode request body")
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	log = log.WithValues("memoryName", req.Name, "provider", req.Provider.Type)
	log.Info("Received request to create memory")

	log.V(1).Info("Checking if memory already exists")
	existingMemory := &v1alpha1.Memory{}
	err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      req.Name,
		Namespace: common.GetResourceNamespace(),
	}, existingMemory)
	if err == nil {
		log.Info("Memory already exists")
		w.RespondWithError(errors.NewConflictError("Memory already exists", nil))
		return
	} else if !k8serrors.IsNotFound(err) {
		log.Error(err, "Failed to check if memory exists")
		w.RespondWithError(errors.NewInternalServerError("Failed to check if memory exists", err))
		return
	}

	providerTypeEnum := v1alpha1.MemoryProvider(req.Provider.Type)
	memorySpec := v1alpha1.MemorySpec{
		Provider:        providerTypeEnum,
		APIKeySecretRef: req.Name,
		APIKeySecretKey: fmt.Sprintf("%s_API_KEY", strings.ToUpper(req.Provider.Type)),
	}

	if providerTypeEnum == v1alpha1.Pinecone {
		memorySpec.Pinecone = req.PineconeParams
	}

	apiKey := req.APIKey
	_, err = CreateSecret(h.KubeClient, memorySpec.APIKeySecretRef, common.GetResourceNamespace(), map[string]string{memorySpec.APIKeySecretKey: apiKey})
	if err != nil {
		log.Error(err, "Failed to create memory API key secret")
		w.RespondWithError(errors.NewInternalServerError("Failed to create memory API key secret", err))
		return
	}
	log.V(1).Info("Successfully created memory API key secret")
	memory := &v1alpha1.Memory{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: common.GetResourceNamespace(),
		},
		Spec: memorySpec,
	}

	if err := h.KubeClient.Create(r.Context(), memory); err != nil {
		log.Error(err, "Failed to create memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to create memory", err))
		return
	}

	log.Info("Memory created successfully")
	RespondWithJSON(w, http.StatusCreated, memory)
}

func (h *MemoryHandler) HandleDeleteMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "delete")

	configName, err := GetPathParam(r, "memoryName")
	if err != nil {
		log.Error(err, "Failed to get config name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get config name from path", err))
		return
	}
	log = log.WithValues("memoryName", configName)

	log.Info("Received request to delete memory")

	log.V(1).Info("Checking if memory exists")
	existingMemory := &v1alpha1.Memory{}
	err = h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      configName,
		Namespace: common.GetResourceNamespace(),
	}, existingMemory)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Memory not found")
			w.RespondWithError(errors.NewNotFoundError("Memory not found", nil))
			return
		}
		log.Error(err, "Failed to get memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to get memory", err))
		return
	}

	log.Info("Deleting memory")
	if err := h.KubeClient.Delete(r.Context(), existingMemory); err != nil {
		log.Error(err, "Failed to delete memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to delete memory", err))
		return
	}

	log.Info("Memory deleted successfully")
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Memory deleted successfully"})
}

func (h *MemoryHandler) HandleGetMemory(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("memory-handler").WithValues("operation", "get")

	configName, err := GetPathParam(r, "memoryName")
	if err != nil {
		log.Error(err, "Failed to get config name from path")
		w.RespondWithError(errors.NewBadRequestError("Failed to get config name from path", err))
		return
	}
	log = log.WithValues("memoryName", configName)

	log.Info("Received request to get memory")

	memory := &v1alpha1.Memory{}
	err = h.KubeClient.Get(r.Context(), types.NamespacedName{
		Name:      configName,
		Namespace: common.GetResourceNamespace(),
	}, memory)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Memory not found")
			w.RespondWithError(errors.NewNotFoundError("Memory not found", nil))
			return
		}
		log.Error(err, "Failed to get memory")
		w.RespondWithError(errors.NewInternalServerError("Failed to get memory", err))
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
