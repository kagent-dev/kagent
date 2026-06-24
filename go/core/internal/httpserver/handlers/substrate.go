package handlers

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// SubstrateHandler exposes Agent Substrate inventory for the UI.
type SubstrateHandler struct {
	*Base
	AteClient *substrate.Client
}

// NewSubstrateHandler creates a SubstrateHandler.
func NewSubstrateHandler(base *Base, ateClient *substrate.Client) *SubstrateHandler {
	return &SubstrateHandler{Base: base, AteClient: ateClient}
}

// HandleGetSubstrateStatus handles GET /api/substrate/status?namespace=…
func (h *SubstrateHandler) HandleGetSubstrateStatus(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("substrate-handler").WithValues("operation", "status")
	if err := Check(h.Authorizer, r, auth.Resource{Type: "Agent"}); err != nil {
		w.RespondWithError(err)
		return
	}

	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if namespace != "" {
		if errs := utilvalidation.IsDNS1123Label(namespace); len(errs) > 0 {
			w.RespondWithError(errors.NewBadRequestError(
				fmt.Sprintf("invalid namespace %q: %s", namespace, strings.Join(errs, ", ")),
				nil,
			))
			return
		}
	}

	namespaces, err := h.substrateNamespaces(namespace)
	if err != nil {
		w.RespondWithError(err)
		return
	}

	resp := api.SubstrateStatusResponse{
		Enabled:        h.AteClient != nil,
		WorkerPools:    []api.SubstrateWorkerPoolEntry{},
		ActorTemplates: []api.SubstrateActorTemplateEntry{},
		Actors:         []api.SubstrateActorEntry{},
		Workers:        []api.SubstrateWorkerEntry{},
	}

	if h.AteClient != nil {
		for _, ns := range namespaces {
			wpEntries, tmplEntries, err := h.listSubstrateCRs(r.Context(), ns)
			if err != nil {
				log.Error(err, "list substrate CRs", "namespace", ns)
				w.RespondWithError(errors.NewInternalServerError("Failed to list substrate resources from Kubernetes", err))
				return
			}
			resp.WorkerPools = append(resp.WorkerPools, wpEntries...)
			resp.ActorTemplates = append(resp.ActorTemplates, tmplEntries...)
		}

		actors, workers, ateErr := h.listAteAPIState(r.Context(), namespaces)
		resp.Actors = actors
		resp.Workers = workers
		if ateErr != nil {
			resp.AteAPIError = ateErr.Error()
			log.Error(ateErr, "list ate-api state")
		}
	}

	slices.SortStableFunc(resp.WorkerPools, compareWorkerPool)
	slices.SortStableFunc(resp.ActorTemplates, compareActorTemplate)
	slices.SortStableFunc(resp.Actors, compareActor)
	slices.SortStableFunc(resp.Workers, compareWorker)

	data := api.NewResponse(resp, "Successfully listed substrate status", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *SubstrateHandler) substrateNamespaces(requested string) ([]string, error) {
	if requested != "" {
		return []string{requested}, nil
	}
	if len(h.WatchedNamespaces) > 0 {
		return slices.Clone(h.WatchedNamespaces), nil
	}
	return []string{""}, nil
}

func (h *SubstrateHandler) listSubstrateCRs(ctx context.Context, namespace string) ([]api.SubstrateWorkerPoolEntry, []api.SubstrateActorTemplateEntry, error) {
	var listOpts []client.ListOption
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}

	wpList := &atev1alpha1.WorkerPoolList{}
	if err := h.KubeClient.List(ctx, wpList, listOpts...); err != nil {
		return nil, nil, err
	}
	tmplList := &atev1alpha1.ActorTemplateList{}
	if err := h.KubeClient.List(ctx, tmplList, listOpts...); err != nil {
		return nil, nil, err
	}

	workerPools := make([]api.SubstrateWorkerPoolEntry, 0, len(wpList.Items))
	for i := range wpList.Items {
		wp := &wpList.Items[i]
		workerPools = append(workerPools, api.SubstrateWorkerPoolEntry{
			Namespace:  wp.Namespace,
			Name:       wp.Name,
			Replicas:   wp.Spec.Replicas,
			AteomImage: wp.Spec.AteomImage,
		})
	}

	templates := make([]api.SubstrateActorTemplateEntry, 0, len(tmplList.Items))
	for i := range tmplList.Items {
		tmpl := &tmplList.Items[i]
		entry := api.SubstrateActorTemplateEntry{
			Namespace:       tmpl.Namespace,
			Name:            tmpl.Name,
			Phase:           string(tmpl.Status.Phase),
			GoldenActorID:   tmpl.Status.GoldenActorID,
			GoldenSnapshot:  tmpl.Status.GoldenSnapshot,
			ManagedByKagent: tmpl.Labels["app.kubernetes.io/managed-by"] == "kagent",
		}
		if harness := strings.TrimSpace(tmpl.Labels[substrate.HarnessLabelKey]); harness != "" {
			entry.HarnessName = harness
		} else if agentName := substrate.SandboxAgentNameFromLabels(tmpl.Labels); agentName != "" {
			entry.HarnessName = agentName
		}
		if ref := tmpl.Spec.WorkerPoolRef; ref.Name != "" {
			wpNS := ref.Namespace
			if wpNS == "" {
				wpNS = tmpl.Namespace
			}
			entry.WorkerPoolRef = wpNS + "/" + ref.Name
		}
		templates = append(templates, entry)
	}

	return workerPools, templates, nil
}

func (h *SubstrateHandler) listAteAPIState(ctx context.Context, namespaces []string) ([]api.SubstrateActorEntry, []api.SubstrateWorkerEntry, error) {
	allowAll := len(namespaces) == 1 && namespaces[0] == ""
	allowed := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		if ns != "" {
			allowed[ns] = struct{}{}
		}
	}

	actorPB, err := h.AteClient.ListActors(ctx)
	if err != nil {
		return nil, nil, err
	}
	workerPB, err := h.AteClient.ListWorkers(ctx)
	if err != nil {
		return nil, nil, err
	}

	actors := make([]api.SubstrateActorEntry, 0, len(actorPB))
	for _, a := range actorPB {
		if a == nil {
			continue
		}
		ns := strings.TrimSpace(a.GetActorTemplateNamespace())
		if !allowAll && ns != "" {
			if _, ok := allowed[ns]; !ok {
				continue
			}
		}
		actors = append(actors, actorEntryFromPB(a))
	}

	workers := make([]api.SubstrateWorkerEntry, 0, len(workerPB))
	for _, w := range workerPB {
		if w == nil {
			continue
		}
		ns := strings.TrimSpace(w.GetWorkerNamespace())
		if !allowAll && ns != "" {
			if _, ok := allowed[ns]; !ok {
				continue
			}
		}
		workers = append(workers, workerEntryFromPB(w))
	}

	return actors, workers, nil
}

func actorEntryFromPB(a *ateapipb.Actor) api.SubstrateActorEntry {
	return api.SubstrateActorEntry{
		ActorID:                a.GetActorId(),
		Status:                 substrate.ActorStatusLabel(a.GetStatus()),
		ActorTemplateNamespace: a.GetActorTemplateNamespace(),
		ActorTemplateName:      a.GetActorTemplateName(),
		AteomPodNamespace:      a.GetAteomPodNamespace(),
		AteomPodName:           a.GetAteomPodName(),
		AteomPodIP:             a.GetAteomPodIp(),
		LastSnapshot:           a.GetLastSnapshot(),
		InProgressSnapshot:     a.GetInProgressSnapshot(),
		Version:                a.GetVersion(),
	}
}

func workerEntryFromPB(w *ateapipb.Worker) api.SubstrateWorkerEntry {
	return api.SubstrateWorkerEntry{
		WorkerNamespace: w.GetWorkerNamespace(),
		WorkerPool:      w.GetWorkerPool(),
		WorkerPod:       w.GetWorkerPod(),
		ActorNamespace:  w.GetActorNamespace(),
		ActorTemplate:   w.GetActorTemplate(),
		ActorID:         w.GetActorId(),
		IP:              w.GetIp(),
		Version:         w.GetVersion(),
	}
}

func compareWorkerPool(a, b api.SubstrateWorkerPoolEntry) int {
	return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
}

func compareActorTemplate(a, b api.SubstrateActorTemplateEntry) int {
	return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
}

func compareActor(a, b api.SubstrateActorEntry) int {
	return strings.Compare(a.ActorID, b.ActorID)
}

func compareWorker(a, b api.SubstrateWorkerEntry) int {
	return strings.Compare(a.WorkerNamespace+"/"+a.WorkerPool+"/"+a.WorkerPod, b.WorkerNamespace+"/"+b.WorkerPool+"/"+b.WorkerPod)
}
