package translator

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

// CronAgentTranslator translates CronAgent CRs to Kubernetes resources
type CronAgentTranslator struct {
	AgentTranslator agent.AdkApiTranslator
}

// NewCronAgentTranslator creates a new CronAgentTranslator
func NewCronAgentTranslator(agentTranslator agent.AdkApiTranslator) *CronAgentTranslator {
	return &CronAgentTranslator{
		AgentTranslator: agentTranslator,
	}
}

// TranslateToCronJob converts a CronAgent CR to a Kubernetes CronJob
func (t *CronAgentTranslator) TranslateToCronJob(cronAgent *v1alpha2.CronAgent) (*batchv1.CronJob, error) {
	// Convert ConcurrencyPolicy
	concurrencyPolicy := batchv1.AllowConcurrent
	if cronAgent.Spec.ConcurrencyPolicy != nil {
		switch *cronAgent.Spec.ConcurrencyPolicy {
		case v1alpha2.ConcurrencyPolicyAllow:
			concurrencyPolicy = batchv1.AllowConcurrent
		case v1alpha2.ConcurrencyPolicyForbid:
			concurrencyPolicy = batchv1.ForbidConcurrent
		case v1alpha2.ConcurrencyPolicyReplace:
			concurrencyPolicy = batchv1.ReplaceConcurrent
		}
	}

	jobTemplate, err := t.translateToJobTemplate(cronAgent)
	if err != nil {
		return nil, err
	}

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronAgent.Name,
			Namespace: cronAgent.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "cronagent",
				"app.kubernetes.io/instance":   cronAgent.Name,
				"app.kubernetes.io/managed-by": "kagent",
			},
			// OwnerReference ensures automatic cleanup when CronAgent is deleted
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cronAgent, v1alpha2.GroupVersion.WithKind("CronAgent")),
			},
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   cronAgent.Spec.Schedule,
			TimeZone:                   cronAgent.Spec.Timezone,
			ConcurrencyPolicy:          concurrencyPolicy,
			StartingDeadlineSeconds:    cronAgent.Spec.StartingDeadlineSeconds,
			SuccessfulJobsHistoryLimit: cronAgent.Spec.SuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     cronAgent.Spec.FailedJobsHistoryLimit,
			Suspend:                    cronAgent.Spec.Suspend,
			JobTemplate:                *jobTemplate,
		},
	}

	return cronJob, nil
}

// TranslateToJob creates a Job for manual triggering
func (t *CronAgentTranslator) TranslateToJob(cronAgent *v1alpha2.CronAgent, timestamp string) (*batchv1.Job, error) {
	jobTemplate, err := t.translateToJobTemplate(cronAgent)
	if err != nil {
		return nil, err
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("cronagent-%s-%s", cronAgent.Name, timestamp),
			Namespace:   cronAgent.Namespace,
			Labels:      jobTemplate.Labels,
			Annotations: jobTemplate.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(cronAgent, v1alpha2.GroupVersion.WithKind("CronAgent")),
			},
		},
		Spec: jobTemplate.Spec,
	}

	return job, nil
}

// translateToJobTemplate creates a JobTemplateSpec from CronAgent
func (t *CronAgentTranslator) translateToJobTemplate(cronAgent *v1alpha2.CronAgent) (*batchv1.JobTemplateSpec, error) {
	// Build environment variables for the agent pod
	envVars := []corev1.EnvVar{
		{
			Name:  "KAGENT_CRONAGENT_NAME",
			Value: cronAgent.Name,
		},
		{
			Name:  "KAGENT_INITIAL_TASK",
			Value: cronAgent.Spec.InitialTask,
		},
		{
			Name:  "KAGENT_THREAD_POLICY",
			Value: string(cronAgent.Spec.ThreadPolicy),
		},
		{
			Name:  "KAGENT_USER_ID",
			Value: "system", // Default system user for CronAgent runs
		},
	}

	// Create a synthetic Agent CR from the AgentTemplate
	// This allows us to reuse the AgentTranslator logic
	syntheticAgent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cronagent-%s-%d", cronAgent.Name, time.Now().Unix()),
			Namespace: cronAgent.Namespace,
		},
		Spec: cronAgent.Spec.AgentTemplate,
	}

	// Use AgentTranslator to get the base outputs
	ctx := context.Background()
	outputs, err := t.AgentTranslator.TranslateAgent(ctx, syntheticAgent)
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent template: %w", err)
	}

	// Extract the Deployment from manifests
	var podSpec corev1.PodSpec
	for _, obj := range outputs.Manifest {
		if dep, ok := obj.(*appsv1.Deployment); ok {
			if len(dep.Spec.Template.Spec.Containers) > 0 {
				podSpec = dep.Spec.Template.Spec
				// Merge CronAgent-specific env vars with Agent env vars
				podSpec.Containers[0].Env = append(envVars, podSpec.Containers[0].Env...)
				break
			}
		}
	}

	if len(podSpec.Containers) == 0 {
		return nil, fmt.Errorf("no containers found in agent template translation")
	}

	// Jobs/CronJobs require RestartPolicy to be OnFailure or Never
	// Override the Deployment's default "Always" policy
	podSpec.RestartPolicy = corev1.RestartPolicyOnFailure

	jobTemplate := &batchv1.JobTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app.kubernetes.io/name":       "cronagent",
				"app.kubernetes.io/instance":   cronAgent.Name,
				"app.kubernetes.io/managed-by": "kagent",
				"cronagent.kagent.dev/name":    cronAgent.Name,
			},
			Annotations: map[string]string{
				"kagent.dev/cronagent-name": cronAgent.Name,
				"kagent.dev/thread-policy":  string(cronAgent.Spec.ThreadPolicy),
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":       "cronagent",
						"app.kubernetes.io/instance":   cronAgent.Name,
						"app.kubernetes.io/managed-by": "kagent",
						"cronagent.kagent.dev/name":    cronAgent.Name,
					},
				},
				Spec: podSpec,
			},
			// Jobs should not retry on failure - let CronJob handle scheduling
			BackoffLimit: int32Ptr(0),
		},
	}

	return jobTemplate, nil
}

func int32Ptr(i int32) *int32 {
	return &i
}
