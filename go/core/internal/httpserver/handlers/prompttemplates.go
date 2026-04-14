package handlers

import (
	"cmp"
	"maps"
	"net/http"
	"slices"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// kagent.dev/prompt-library=true marks a ConfigMap as a prompt library for list/@-picker APIs.
	promptLibraryLabelKey = "kagent.dev/prompt-library"
	promptLibraryLabelVal = "true"
)

func promptLibraryLabelSelector() map[string]string {
	return map[string]string{promptLibraryLabelKey: promptLibraryLabelVal}
}

// PromptTemplatesHandler manages ConfigMaps used as prompt template libraries.
type PromptTemplatesHandler struct {
	*Base
}

// NewPromptTemplatesHandler creates a PromptTemplatesHandler.
func NewPromptTemplatesHandler(base *Base) *PromptTemplatesHandler {
	return &PromptTemplatesHandler{Base: base}
}

// HandleListPromptTemplates handles GET /api/prompttemplates?namespace=…
func (h *PromptTemplatesHandler) HandleListPromptTemplates(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("prompttemplates-handler").WithValues("operation", "list")
	if err := Check(h.Authorizer, r, auth.Resource{Type: "PromptTemplate"}); err != nil {
		w.RespondWithError(err)
		return
	}

	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		w.RespondWithError(errors.NewBadRequestError("namespace query parameter is required", nil))
		return
	}

	byName := make(map[string]corev1.ConfigMap)

	list := &corev1.ConfigMapList{}
	if err := h.KubeClient.List(r.Context(), list, client.InNamespace(ns), client.MatchingLabels(promptLibraryLabelSelector())); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list prompt template ConfigMaps", err))
		return
	}
	for i := range list.Items {
		cm := list.Items[i]
		byName[cm.Name] = cm
	}

	out := make([]api.PromptTemplateSummary, 0, len(byName))
	for _, cm := range byName {
		out = append(out, summarizePromptCM(&cm))
	}
	slices.SortFunc(out, func(a, b api.PromptTemplateSummary) int {
		return cmp.Compare(a.Name, b.Name)
	})

	log.Info("Listed prompt template ConfigMaps", "count", len(out))
	RespondWithJSON(w, http.StatusOK, api.NewResponse(out, "Successfully listed prompt template ConfigMaps", false))
}

// HandleGetPromptTemplate handles GET /api/prompttemplates/{namespace}/{name}
func (h *PromptTemplatesHandler) HandleGetPromptTemplate(w ErrorResponseWriter, r *http.Request) {
	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}
	log := ctrllog.FromContext(r.Context()).WithName("prompttemplates-handler").WithValues(
		"operation", "get",
		"namespace", namespace,
		"name", name,
	)

	if err := Check(h.Authorizer, r, auth.Resource{Type: "PromptTemplate", Name: namespace + "/" + name}); err != nil {
		w.RespondWithError(err)
		return
	}

	cm := &corev1.ConfigMap{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("ConfigMap not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get ConfigMap", err))
		return
	}

	detail := api.PromptTemplateDetail{
		Namespace: cm.Namespace,
		Name:      cm.Name,
		Data:      cloneStringMap(cm.Data),
	}
	log.Info("Retrieved prompt template library")
	RespondWithJSON(w, http.StatusOK, api.NewResponse(detail, "Successfully retrieved prompt template library", false))
}

// HandleCreatePromptTemplate handles POST /api/prompttemplates
func (h *PromptTemplatesHandler) HandleCreatePromptTemplate(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("prompttemplates-handler").WithValues("operation", "create")
	if err := Check(h.Authorizer, r, auth.Resource{Type: "PromptTemplate"}); err != nil {
		w.RespondWithError(err)
		return
	}

	var req api.CreatePromptTemplateRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	if errMsg := validatePromptTemplateRequest(req); errMsg != "" {
		w.RespondWithError(errors.NewBadRequestError(errMsg, nil))
		return
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
			Labels:    promptLibraryLabelSelector(),
		},
		Data: cloneStringMap(req.Data),
	}

	if err := h.KubeClient.Create(r.Context(), cm); err != nil {
		if apierrors.IsAlreadyExists(err) {
			w.RespondWithError(errors.NewBadRequestError("A ConfigMap with this name already exists in the namespace", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to create ConfigMap", err))
		return
	}

	log.Info("Created prompt template library", "namespace", req.Namespace, "name", req.Name)
	detail := api.PromptTemplateDetail{
		Namespace: cm.Namespace,
		Name:      cm.Name,
		Data:      cloneStringMap(cm.Data),
	}
	RespondWithJSON(w, http.StatusCreated, api.NewResponse(detail, "Successfully created prompt template library", false))
}

// HandleUpdatePromptTemplate handles PUT /api/prompttemplates/{namespace}/{name}
func (h *PromptTemplatesHandler) HandleUpdatePromptTemplate(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("prompttemplates-handler").WithValues("operation", "update")
	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	if err := Check(h.Authorizer, r, auth.Resource{Type: "PromptTemplate", Name: namespace + "/" + name}); err != nil {
		w.RespondWithError(err)
		return
	}

	var req api.UpdatePromptTemplateRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}
	if len(req.Data) == 0 {
		w.RespondWithError(errors.NewBadRequestError("at least one template key is required", nil))
		return
	}

	cm := &corev1.ConfigMap{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("ConfigMap not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get ConfigMap", err))
		return
	}

	cm.Data = cloneStringMap(req.Data)
	if err := h.KubeClient.Update(r.Context(), cm); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to update ConfigMap", err))
		return
	}

	log.Info("Updated prompt template library", "namespace", namespace, "name", name)
	detail := api.PromptTemplateDetail{
		Namespace: cm.Namespace,
		Name:      cm.Name,
		Data:      cloneStringMap(cm.Data),
	}
	RespondWithJSON(w, http.StatusOK, api.NewResponse(detail, "Successfully updated prompt template library", false))
}

// HandleDeletePromptTemplate handles DELETE /api/prompttemplates/{namespace}/{name}
func (h *PromptTemplatesHandler) HandleDeletePromptTemplate(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("prompttemplates-handler").WithValues("operation", "delete")
	namespace, err := GetPathParam(r, "namespace")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get namespace from path", err))
		return
	}
	name, err := GetPathParam(r, "name")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get name from path", err))
		return
	}

	if err := Check(h.Authorizer, r, auth.Resource{Type: "PromptTemplate", Name: namespace + "/" + name}); err != nil {
		w.RespondWithError(err)
		return
	}

	cm := &corev1.ConfigMap{}
	if err := h.KubeClient.Get(r.Context(), client.ObjectKey{Namespace: namespace, Name: name}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			w.RespondWithError(errors.NewNotFoundError("ConfigMap not found", err))
			return
		}
		w.RespondWithError(errors.NewInternalServerError("Failed to get ConfigMap", err))
		return
	}

	if err := h.KubeClient.Delete(r.Context(), cm); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to delete ConfigMap", err))
		return
	}

	log.Info("Deleted prompt template library", "namespace", namespace, "name", name)
	RespondWithJSON(w, http.StatusOK, api.NewResponse(struct{}{}, "Successfully deleted prompt template library", false))
}

func summarizePromptCM(cm *corev1.ConfigMap) api.PromptTemplateSummary {
	keyCount := len(cm.Data)
	for range cm.BinaryData {
		keyCount++ // surface presence; binary keys are not editable in UI
	}
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return api.PromptTemplateSummary{
		Namespace: cm.Namespace,
		Name:      cm.Name,
		KeyCount:  keyCount,
		Keys:      keys,
	}
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return maps.Clone(m)
}

func validatePromptTemplateRequest(req api.CreatePromptTemplateRequest) string {
	if req.Namespace == "" {
		return "namespace is required"
	}
	if errs := utilvalidation.IsDNS1123Subdomain(req.Namespace); len(errs) > 0 {
		return "namespace must be a valid DNS subdomain"
	}
	if req.Name == "" {
		return "name is required"
	}
	if errs := utilvalidation.IsDNS1123Subdomain(req.Name); len(errs) > 0 {
		return "name must be a valid DNS subdomain"
	}
	if len(req.Data) == 0 {
		return "at least one template key is required"
	}
	for k := range req.Data {
		if k == "" {
			return "template keys cannot be empty"
		}
	}
	return ""
}
