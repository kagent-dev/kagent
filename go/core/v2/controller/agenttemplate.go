package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/v2/substrate"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// revisionPendingRequeue polls Substrate while it builds the golden snapshot.
// ActorTemplate status changes also trigger reconciliation, but polling covers
// missed watch events and implementations that update status slowly.
const revisionPendingRequeue = 2 * time.Second

// +kubebuilder:rbac:groups=kagent.dev,resources=agenttemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=kagent.dev,resources=agenttemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=harnesses;modelconfigs;remotemcpservers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps;secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=ate.dev,resources=workerpools,verbs=get;list;watch
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates/status,verbs=get

// AgentTemplateController reconciles each AgentTemplate/Harness pair
// independently. One AgentTemplate may therefore have several runtime
// revisions and a different readiness result for each Harness.
type AgentTemplateController struct {
	Client     client.Client
	Translator *v2translator.Compiler
	Lifecycle  *substrate.Lifecycle
	Store      runtimeRevisionStore
}

// runtimeRevisionStore is the controller's narrow view of the shared database.
// The database records which immutable runtime revisions are still referenced;
// Kubernetes remains the source of truth for the ActorTemplates themselves.
type runtimeRevisionStore interface {
	UpsertAgentTemplateHarnessPair(context.Context, dbpkg.AgentTemplateHarnessPair) error
	UpsertRuntimeRevision(context.Context, dbpkg.RuntimeRevision) error
	MarkRuntimeRevisionSuccessful(context.Context, dbpkg.AgentTemplateHarnessPair) error
	RetireAgentTemplateHarnessPairs(context.Context, string, string) error
	RetireAgentTemplateHarnessPair(context.Context, string, string, string) error
	RetireOtherAgentTemplateHarnessPairs(context.Context, string, string, []string) error
	ListUnreferencedRuntimeRevisions(context.Context) ([]dbpkg.RuntimeRevisionRef, error)
	DeleteUnreferencedRuntimeRevision(context.Context, string) error
}

// Reconcile converges all Harness pairs for one AgentTemplate, then retires
// pairs and immutable revisions no longer referenced by spec.
func (r *AgentTemplateController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	template := &v1alpha3.AgentTemplate{}
	if err := r.Client.Get(ctx, req.NamespacedName, template); err != nil {
		if apierrors.IsNotFound(err) {
			// A deleted API object can no longer carry finalizers or status. Retire
			// its database pairs and collect any now-unreferenced revisions.
			if err := r.Store.RetireAgentTemplateHarnessPairs(ctx, req.Namespace, req.Name); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, r.cleanupUnreferencedRevisions(ctx)
		}
		return ctrl.Result{}, fmt.Errorf("get AgentTemplate %s: %w", req.NamespacedName, err)
	}

	// Preserve per-Harness status while rebuilding the ordered status list from
	// spec. This also drops status entries for Harnesses removed from spec.
	existing := make(map[string]v1alpha3.AgentTemplateHarnessStatus, len(template.Status.Harnesses))
	for _, status := range template.Status.Harnesses {
		existing[status.Harness] = status
	}
	statuses := make([]v1alpha3.AgentTemplateHarnessStatus, 0, len(template.Spec.Harnesses.Include))
	harnessNames := make([]string, 0, len(template.Spec.Harnesses.Include))
	pending := false
	for index, include := range template.Spec.Harnesses.Include {
		harnessNames = append(harnessNames, include.Name)
		status, waiting, err := r.reconcilePair(ctx, template, include.Name, existing[include.Name])
		for i := range status.Conditions {
			status.Conditions[i].ObservedGeneration = template.Generation
		}
		statuses = append(statuses, status)
		pending = pending || waiting
		if err != nil {
			// Keep untouched Harness statuses when one pair hits a transient
			// error; otherwise a failed reconcile would erase useful status.
			for _, remaining := range template.Spec.Harnesses.Include[index+1:] {
				status := existing[remaining.Name]
				status.Harness = remaining.Name
				if status.DesiredRevision == "" {
					status.DesiredRevision = requestedRevision(template, remaining.Name)
				}
				statuses = append(statuses, status)
			}
			if patchErr := r.patchStatus(ctx, template, statuses); patchErr != nil {
				return ctrl.Result{}, errors.Join(err, patchErr)
			}
			return ctrl.Result{}, err
		}
	}
	if err := r.patchStatus(ctx, template, statuses); err != nil {
		return ctrl.Result{}, err
	}
	// Status is patched before retiring old pairs so users never observe
	// database cleanup as a successful spec transition without matching status.
	if err := r.Store.RetireOtherAgentTemplateHarnessPairs(ctx, template.Namespace, string(template.UID), harnessNames); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.cleanupUnreferencedRevisions(ctx); err != nil {
		return ctrl.Result{}, err
	}
	if pending {
		return ctrl.Result{RequeueAfter: revisionPendingRequeue}, nil
	}
	return ctrl.Result{}, nil
}

// cleanupUnreferencedRevisions removes ActorTemplates only after the database
// proves that no AgentTemplate/Harness pair references their revision.
func (r *AgentTemplateController) cleanupUnreferencedRevisions(ctx context.Context) error {
	revisions, err := r.Store.ListUnreferencedRuntimeRevisions(ctx)
	if err != nil {
		return err
	}
	for _, stored := range revisions {
		// Delete compute first. Keeping a failed database row makes cleanup
		// retryable; deleting the row first could orphan an ActorTemplate.
		if err := r.Lifecycle.DeleteActorTemplate(ctx, substrate.ActorTemplateRef{
			Namespace: stored.ActorTemplateNamespace, Name: stored.ActorTemplateName,
			UID: stored.ActorTemplateUID, Phase: stored.Phase, GoldenSnapshot: stored.GoldenSnapshot,
		}); err != nil {
			return err
		}
		if err := r.Store.DeleteUnreferencedRuntimeRevision(ctx, stored.Revision); err != nil {
			return err
		}
	}
	return nil
}

// reconcilePair admits, compiles, provisions, and reports one
// AgentTemplate/Harness pair without coupling its status to sibling Harnesses.
func (r *AgentTemplateController) reconcilePair(ctx context.Context, template *v1alpha3.AgentTemplate, harnessName string, previous v1alpha3.AgentTemplateHarnessStatus) (v1alpha3.AgentTemplateHarnessStatus, bool, error) {
	status := previous
	status.Harness = harnessName
	status.DesiredRevision = requestedRevision(template, harnessName)

	harness := &v1alpha3.Harness{}
	key := types.NamespacedName{Namespace: template.Namespace, Name: harnessName}
	if err := r.Client.Get(ctx, key, harness); err != nil {
		setHarnessFailure(&status, "HarnessNotFound", fmt.Sprintf("resolve Harness %q: %v", harnessName, err), v1alpha3.AgentTemplateConditionResolvedRefs)
		if apierrors.IsNotFound(err) {
			if retireErr := r.Store.RetireAgentTemplateHarnessPair(ctx, template.Namespace, template.Name, harnessName); retireErr != nil {
				return status, false, retireErr
			}
			return status, false, nil
		}
		return status, false, err
	}

	// Harness admission is checked before resolving credentials or creating any
	// runtime resources for the pair.
	if err := admitsAgentTemplate(harness, template); err != nil {
		setHarnessFailure(&status, "PairRejected", err.Error(), v1alpha3.AgentTemplateConditionAccepted)
		if retireErr := r.Store.RetireAgentTemplateHarnessPair(ctx, template.Namespace, template.Name, harnessName); retireErr != nil {
			return status, false, retireErr
		}
		return status, false, nil
	}
	setHarnessCondition(&status, v1alpha3.AgentTemplateConditionAccepted, metav1.ConditionTrue, "Accepted", "Harness and AgentTemplate admit the pair")

	// Compilation resolves every referenced object and credential into the
	// complete input set whose digest identifies the immutable revision.
	revisionSpec, err := r.Translator.CompileAgentTemplate(ctx, harness, template)
	if err != nil {
		var validation *v2translator.ValidationError
		if errors.As(err, &validation) {
			setHarnessFailure(&status, "UnsupportedConfiguration", err.Error(), v1alpha3.AgentTemplateConditionCompatible)
			return status, false, nil
		}
		setHarnessFailure(&status, "ReferenceResolutionFailed", err.Error(), v1alpha3.AgentTemplateConditionResolvedRefs)
		return status, false, nil
	}
	revisionID, err := revisionSpec.Digest()
	if err != nil {
		return status, false, err
	}
	status.DesiredRevision = revisionID
	pair := dbpkg.AgentTemplateHarnessPair{
		Namespace: template.Namespace, AgentTemplateName: template.Name, AgentTemplateUID: string(template.UID),
		HarnessName: harness.Name, HarnessUID: string(harness.UID), DesiredRevision: revisionID,
	}
	// Record the desired edge before provisioning so garbage collection cannot
	// mistake this revision for an unreferenced object during reconciliation.
	if err := r.Store.UpsertAgentTemplateHarnessPair(ctx, pair); err != nil {
		return status, false, fmt.Errorf("store AgentTemplate/Harness pair: %w", err)
	}

	templateRef, err := r.Lifecycle.EnsureActorTemplate(ctx, revisionSpec, revisionID)
	if err != nil {
		setHarnessFailure(&status, "ProvisioningFailed", err.Error(), v1alpha3.AgentTemplateConditionReady)
		return status, false, err
	}
	// Persist the actual Kubernetes identity returned by Substrate. Future
	// reconciles and garbage collection must use the UID, not only the name.
	if err := r.Store.UpsertRuntimeRevision(ctx, dbpkg.RuntimeRevision{
		Revision: revisionID, Namespace: template.Namespace, AgentTemplateName: template.Name,
		AgentTemplateUID: string(template.UID), HarnessName: harness.Name, HarnessUID: string(harness.UID),
		SourceSnapshot: revisionSpec.SourceSnapshot, EgressDestinations: revisionSpec.EgressDestinations,
		ActorTemplateNamespace: templateRef.Namespace, ActorTemplateName: templateRef.Name,
		ActorTemplateUID: templateRef.UID, Phase: templateRef.Phase, GoldenSnapshot: templateRef.GoldenSnapshot,
	}); err != nil {
		return status, false, err
	}
	setHarnessCondition(&status, v1alpha3.AgentTemplateConditionResolvedRefs, metav1.ConditionTrue, "Resolved", "All runtime references resolved")
	setHarnessCondition(&status, v1alpha3.AgentTemplateConditionCompatible, metav1.ConditionTrue, "Compatible", "Resolved configuration is compatible with the Harness")
	// Creation alone is not readiness: Substrate must finish the golden snapshot
	// from which new Actors will start.
	if templateRef.Phase != string(atev1alpha1.PhaseReady) {
		setHarnessCondition(&status, v1alpha3.AgentTemplateConditionReady, metav1.ConditionFalse, "ActorTemplatePending", "waiting for the ActorTemplate golden snapshot")
		return status, true, nil
	}
	if err := r.Store.MarkRuntimeRevisionSuccessful(ctx, pair); err != nil {
		return status, false, fmt.Errorf("mark runtime revision successful: %w", err)
	}
	status.LatestSuccessfulRevision = revisionID
	setHarnessCondition(&status, v1alpha3.AgentTemplateConditionReady, metav1.ConditionTrue, "Ready", "ActorTemplate golden snapshot is ready")
	return status, false, nil
}

// admitsAgentTemplate applies the Harness-owned label selector to the candidate
// AgentTemplate. A nil policy intentionally admits nothing.
func admitsAgentTemplate(harness *v1alpha3.Harness, template *v1alpha3.AgentTemplate) error {
	if harness.Spec.AllowedAgentTemplates == nil {
		return fmt.Errorf("harness %q admits no AgentTemplates", harness.Name)
	}
	selector, err := metav1.LabelSelectorAsSelector(&harness.Spec.AllowedAgentTemplates.Selector)
	if err != nil {
		return fmt.Errorf("invalid Harness admission selector: %w", err)
	}
	if !selector.Matches(labels.Set(template.Labels)) {
		return fmt.Errorf("harness %q admission selector does not match AgentTemplate %q", harness.Name, template.Name)
	}
	return nil
}

// requestedRevision returns a provisional identity available before external
// references can be resolved.
func requestedRevision(template *v1alpha3.AgentTemplate, harness string) string {
	// This is only a stable desired-state marker while referenced objects are
	// unresolved. Once compilation succeeds, Revision.Digest replaces it with
	// the identity of the complete resolved runtime configuration.
	raw, _ := json.Marshal(struct {
		UID        types.UID                  `json:"uid"`
		Generation int64                      `json:"generation"`
		Spec       v1alpha3.AgentTemplateSpec `json:"spec"`
		Harness    string                     `json:"harness"`
	}{template.UID, template.Generation, template.Spec, harness})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func setHarnessFailure(status *v1alpha3.AgentTemplateHarnessStatus, reason, message, failedCondition string) {
	// Conditions form an ordered pipeline. A failure marks the failing stage and
	// makes later stages explicitly blocked instead of leaving stale True values.
	for _, condition := range []string{
		v1alpha3.AgentTemplateConditionAccepted,
		v1alpha3.AgentTemplateConditionResolvedRefs,
		v1alpha3.AgentTemplateConditionCompatible,
		v1alpha3.AgentTemplateConditionReady,
	} {
		conditionReason := "Blocked"
		conditionMessage := "blocked by " + failedCondition
		if condition == failedCondition {
			conditionReason = reason
			conditionMessage = message
		}
		setHarnessCondition(status, condition, metav1.ConditionFalse, conditionReason, conditionMessage)
	}
}

func setHarnessCondition(status *v1alpha3.AgentTemplateHarnessStatus, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type: conditionType, Status: conditionStatus, Reason: reason, Message: message,
		ObservedGeneration: 0, LastTransitionTime: metav1.Now(),
	})
}

func (r *AgentTemplateController) patchStatus(ctx context.Context, template *v1alpha3.AgentTemplate, statuses []v1alpha3.AgentTemplateHarnessStatus) error {
	base := template.DeepCopy()
	template.Status.ObservedGeneration = template.Generation
	template.Status.Harnesses = statuses
	if err := r.Client.Status().Patch(ctx, template, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("patch AgentTemplate status: %w", err)
	}
	return nil
}

func (r *AgentTemplateController) enqueueAgentTemplatesInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	// References are namespace-local. Requeueing the namespace avoids maintaining
	// several secondary indexes while the v2 API is still being established.
	list := &v1alpha3.AgentTemplateList{}
	if err := r.Client.List(ctx, list, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return requests
}

func (r *AgentTemplateController) enqueueRevisionActorTemplate(_ context.Context, obj client.Object) []reconcile.Request {
	// Revision ActorTemplates carry their owning public AgentTemplate in a label,
	// allowing Substrate status changes to drive the public readiness condition.
	name := obj.GetLabels()[substrate.RevisionAgentTemplateLabel]
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: name}}}
}

// SetupWithManager watches every namespaced input that can affect compilation
// and the Substrate status that determines revision readiness.
func (r *AgentTemplateController) SetupWithManager(mgr ctrl.Manager) error {
	allInNamespace := handler.EnqueueRequestsFromMapFunc(r.enqueueAgentTemplatesInNamespace)
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha3.AgentTemplate{}).
		Watches(&v1alpha3.Harness{}, allInNamespace).
		Watches(&v1alpha3.ModelConfig{}, allInNamespace).
		Watches(&v1alpha3.RemoteMCPServer{}, allInNamespace).
		Watches(&corev1.ConfigMap{}, allInNamespace).
		Watches(&corev1.Secret{}, allInNamespace).
		Watches(&atev1alpha1.ActorTemplate{}, handler.EnqueueRequestsFromMapFunc(r.enqueueRevisionActorTemplate)).
		Complete(r)
}
