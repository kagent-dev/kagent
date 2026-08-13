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
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
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

const preparationPendingRequeue = 2 * time.Second

// +kubebuilder:rbac:groups=kagent.dev,resources=agenttemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=kagent.dev,resources=agenttemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagent.dev,resources=harnesses;modelconfigs;remotemcpservers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps;secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=ate.dev,resources=workerpools,verbs=get;list;watch
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=ate.dev,resources=actortemplates/status,verbs=get

type AgentTemplateController struct {
	Client     client.Client
	Translator agent.AdkApiTranslator
	Lifecycle  *substrate.Lifecycle
	Store      database.PreparedRevisionStore
}

func (r *AgentTemplateController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	template := &v1alpha3.AgentTemplate{}
	if err := r.Client.Get(ctx, req.NamespacedName, template); err != nil {
		if apierrors.IsNotFound(err) {
			if r.Store == nil {
				return ctrl.Result{}, nil
			}
			if err := r.Store.RetireAgentTemplateAttachments(ctx, req.Namespace, req.Name); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, r.cleanupUnreferencedRevisions(ctx)
		}
		return ctrl.Result{}, fmt.Errorf("get AgentTemplate %s: %w", req.NamespacedName, err)
	}

	existing := make(map[string]v1alpha3.AgentTemplatePreparationStatus, len(template.Status.Preparations))
	for _, status := range template.Status.Preparations {
		existing[status.Harness] = status
	}
	statuses := make([]v1alpha3.AgentTemplatePreparationStatus, 0, len(template.Spec.Harnesses.Include))
	harnessNames := make([]string, 0, len(template.Spec.Harnesses.Include))
	pending := false
	for index, include := range template.Spec.Harnesses.Include {
		harnessNames = append(harnessNames, include.Name)
		status, waiting, err := r.reconcileAttachment(ctx, template, include.Name, existing[include.Name])
		for i := range status.Conditions {
			status.Conditions[i].ObservedGeneration = template.Generation
		}
		statuses = append(statuses, status)
		pending = pending || waiting
		if err != nil {
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
	if err := r.Store.RetireOtherHarnessAttachments(ctx, template.Namespace, string(template.UID), harnessNames); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.cleanupUnreferencedRevisions(ctx); err != nil {
		return ctrl.Result{}, err
	}
	if pending {
		return ctrl.Result{RequeueAfter: preparationPendingRequeue}, nil
	}
	return ctrl.Result{}, nil
}

func (r *AgentTemplateController) cleanupUnreferencedRevisions(ctx context.Context) error {
	revisions, err := r.Store.ListUnreferencedPreparedRevisions(ctx)
	if err != nil {
		return err
	}
	for _, revision := range revisions {
		if err := r.Lifecycle.DeletePreparedTemplate(ctx, revision.ActorTemplate); err != nil {
			return err
		}
		if err := r.Store.DeleteUnreferencedPreparedRevision(ctx, revision.Revision); err != nil {
			return err
		}
	}
	return nil
}

func (r *AgentTemplateController) reconcileAttachment(ctx context.Context, template *v1alpha3.AgentTemplate, harnessName string, previous v1alpha3.AgentTemplatePreparationStatus) (v1alpha3.AgentTemplatePreparationStatus, bool, error) {
	status := previous
	status.Harness = harnessName
	status.DesiredRevision = requestedRevision(template, harnessName)

	harness := &v1alpha3.Harness{}
	key := types.NamespacedName{Namespace: template.Namespace, Name: harnessName}
	if err := r.Client.Get(ctx, key, harness); err != nil {
		setPreparationFailure(&status, "HarnessNotFound", fmt.Sprintf("resolve Harness %q: %v", harnessName, err), v1alpha3.AgentTemplateConditionResolvedRefs)
		if apierrors.IsNotFound(err) && r.Store != nil {
			if retireErr := r.Store.RetireHarnessAttachment(ctx, template.Namespace, template.Name, harnessName); retireErr != nil {
				return status, false, retireErr
			}
			return status, false, nil
		}
		return status, false, err
	}

	if err := admitsAgentTemplate(harness, template); err != nil {
		setPreparationFailure(&status, "AttachmentRejected", err.Error(), v1alpha3.AgentTemplateConditionAccepted)
		if retireErr := r.Store.RetireHarnessAttachment(ctx, template.Namespace, template.Name, harnessName); retireErr != nil {
			return status, false, retireErr
		}
		return status, false, nil
	}
	setPreparationCondition(&status, v1alpha3.AgentTemplateConditionAccepted, metav1.ConditionTrue, "Accepted", "Harness and AgentTemplate admit the attachment")

	bundle, err := r.Translator.CompileAgentTemplate(ctx, harness, template)
	if err != nil {
		var validation *agent.ValidationError
		if errors.As(err, &validation) {
			setPreparationFailure(&status, "UnsupportedConfiguration", err.Error(), v1alpha3.AgentTemplateConditionCompatible)
			return status, false, nil
		}
		setPreparationFailure(&status, "ReferenceResolutionFailed", err.Error(), v1alpha3.AgentTemplateConditionResolvedRefs)
		return status, false, nil
	}
	revision, err := bundle.Revision()
	if err != nil {
		return status, false, err
	}
	status.DesiredRevision = revision
	attachment := database.PreparedAttachment{
		Namespace: template.Namespace, AgentTemplateName: template.Name, AgentTemplateUID: string(template.UID),
		HarnessName: harness.Name, HarnessUID: string(harness.UID), DesiredRevision: revision,
	}
	if err := r.Store.UpsertPreparedAttachment(ctx, attachment); err != nil {
		return status, false, fmt.Errorf("store prepared attachment: %w", err)
	}

	templateRef, err := r.Lifecycle.EnsurePreparedTemplate(ctx, bundle, revision)
	if err != nil {
		setPreparationFailure(&status, "ProvisioningFailed", err.Error(), v1alpha3.AgentTemplateConditionPrepared)
		return status, false, err
	}
	if err := r.Store.UpsertPreparedRevision(ctx, database.PreparedRevision{
		Revision: revision, Namespace: template.Namespace, AgentTemplateName: template.Name,
		AgentTemplateUID: string(template.UID), HarnessName: harness.Name, HarnessUID: string(harness.UID),
		SourceSnapshot: bundle.SourceSnapshot, EgressDestinations: bundle.EgressDestinations, ActorTemplate: *templateRef,
	}); err != nil {
		return status, false, err
	}
	setPreparationCondition(&status, v1alpha3.AgentTemplateConditionResolvedRefs, metav1.ConditionTrue, "Resolved", "All preparation references resolved")
	setPreparationCondition(&status, v1alpha3.AgentTemplateConditionCompatible, metav1.ConditionTrue, "Compatible", "Resolved configuration is compatible with the Harness")
	if templateRef.Phase != string(atev1alpha1.PhaseReady) {
		setPreparationCondition(&status, v1alpha3.AgentTemplateConditionPrepared, metav1.ConditionFalse, "ActorTemplatePending", "waiting for the ActorTemplate golden snapshot")
		return status, true, nil
	}
	if err := r.Store.MarkPreparedRevisionSuccessful(ctx, attachment); err != nil {
		return status, false, fmt.Errorf("mark prepared revision successful: %w", err)
	}
	status.LatestSuccessfulRevision = revision
	setPreparationCondition(&status, v1alpha3.AgentTemplateConditionPrepared, metav1.ConditionTrue, "Prepared", "ActorTemplate golden snapshot is ready")
	return status, false, nil
}

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

func requestedRevision(template *v1alpha3.AgentTemplate, harness string) string {
	raw, _ := json.Marshal(struct {
		UID        types.UID                  `json:"uid"`
		Generation int64                      `json:"generation"`
		Spec       v1alpha3.AgentTemplateSpec `json:"spec"`
		Harness    string                     `json:"harness"`
	}{template.UID, template.Generation, template.Spec, harness})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func setPreparationFailure(status *v1alpha3.AgentTemplatePreparationStatus, reason, message, failedCondition string) {
	for _, condition := range []string{
		v1alpha3.AgentTemplateConditionAccepted,
		v1alpha3.AgentTemplateConditionResolvedRefs,
		v1alpha3.AgentTemplateConditionCompatible,
		v1alpha3.AgentTemplateConditionPrepared,
	} {
		conditionReason := "Blocked"
		conditionMessage := "blocked by " + failedCondition
		if condition == failedCondition {
			conditionReason = reason
			conditionMessage = message
		}
		setPreparationCondition(status, condition, metav1.ConditionFalse, conditionReason, conditionMessage)
	}
}

func setPreparationCondition(status *v1alpha3.AgentTemplatePreparationStatus, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type: conditionType, Status: conditionStatus, Reason: reason, Message: message,
		ObservedGeneration: 0, LastTransitionTime: metav1.Now(),
	})
}

func (r *AgentTemplateController) patchStatus(ctx context.Context, template *v1alpha3.AgentTemplate, statuses []v1alpha3.AgentTemplatePreparationStatus) error {
	base := template.DeepCopy()
	template.Status.ObservedGeneration = template.Generation
	template.Status.Preparations = statuses
	if err := r.Client.Status().Patch(ctx, template, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("patch AgentTemplate status: %w", err)
	}
	return nil
}

func (r *AgentTemplateController) enqueueAgentTemplatesInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
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

func (r *AgentTemplateController) enqueuePreparedActorTemplate(_ context.Context, obj client.Object) []reconcile.Request {
	name := obj.GetLabels()[substrate.PreparedAgentTemplateLabel]
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: name}}}
}

func (r *AgentTemplateController) SetupWithManager(mgr ctrl.Manager) error {
	if r.Client == nil || r.Translator == nil || r.Lifecycle == nil || r.Store == nil {
		return fmt.Errorf("AgentTemplate controller dependencies are required")
	}
	allInNamespace := handler.EnqueueRequestsFromMapFunc(r.enqueueAgentTemplatesInNamespace)
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha3.AgentTemplate{}).
		Watches(&v1alpha3.Harness{}, allInNamespace).
		Watches(&v1alpha3.ModelConfig{}, allInNamespace).
		Watches(&v1alpha3.RemoteMCPServer{}, allInNamespace).
		Watches(&corev1.ConfigMap{}, allInNamespace).
		Watches(&corev1.Secret{}, allInNamespace).
		Watches(&atev1alpha1.ActorTemplate{}, handler.EnqueueRequestsFromMapFunc(r.enqueuePreparedActorTemplate)).
		Complete(r)
}
