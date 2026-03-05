package translator

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/translator"
)

// mockAgentTranslator is a mock implementation for testing
type mockAgentTranslator struct{}

func (m *mockAgentTranslator) TranslateAgent(ctx context.Context, ag *v1alpha2.Agent) (*translator.AgentOutputs, error) {
	// Return a simple mock deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ag.Name,
			Namespace: ag.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "agent",
							Image: "test-image:latest",
							Env: []corev1.EnvVar{
								{Name: "EXISTING_VAR", Value: "existing_value"},
							},
						},
					},
				},
			},
		},
	}

	return &translator.AgentOutputs{
		Manifest: []client.Object{deployment},
	}, nil
}

func (m *mockAgentTranslator) GetOwnedResourceTypes() []client.Object {
	return []client.Object{}
}

func TestTranslateToCronJob(t *testing.T) {
	tests := []struct {
		name               string
		cronAgent          *v1alpha2.CronAgent
		wantSchedule       string
		wantTimezone       *string
		wantConcurrency    batchv1.ConcurrencyPolicy
		wantSuccessLimit   *int32
		wantFailedLimit    *int32
		wantErrNil         bool
	}{
		{
			name: "basic cronjob",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:     "0 * * * *",
					InitialTask:  "test task",
					ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
					AgentTemplate: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
					},
				},
			},
			wantSchedule:    "0 * * * *",
			wantTimezone:    nil,
			wantConcurrency: batchv1.AllowConcurrent,
			wantErrNil:      true,
		},
		{
			name: "with timezone and concurrency policy",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:     "0 9 * * 1-5",
					Timezone:     stringPtr("America/New_York"),
					InitialTask:  "daily report",
					ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
					ConcurrencyPolicy: func() *v1alpha2.ConcurrencyPolicy {
						p := v1alpha2.ConcurrencyPolicyForbid
						return &p
					}(),
					AgentTemplate: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
					},
				},
			},
			wantSchedule:    "0 9 * * 1-5",
			wantTimezone:    stringPtr("America/New_York"),
			wantConcurrency: batchv1.ForbidConcurrent,
			wantErrNil:      true,
		},
		{
			name: "with history limits",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:     "0 * * * *",
					InitialTask:  "test task",
					ThreadPolicy: v1alpha2.ThreadPolicyPersistent,
					SuccessfulJobsHistoryLimit: int32Ptr(7),
					FailedJobsHistoryLimit:     int32Ptr(3),
					AgentTemplate: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
					},
				},
			},
			wantSchedule:     "0 * * * *",
			wantSuccessLimit: int32Ptr(7),
			wantFailedLimit:  int32Ptr(3),
			wantConcurrency:  batchv1.AllowConcurrent,
			wantErrNil:       true,
		},
		{
			name: "replace concurrency policy",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:     "0 * * * *",
					InitialTask:  "test task",
					ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
					ConcurrencyPolicy: func() *v1alpha2.ConcurrencyPolicy {
						p := v1alpha2.ConcurrencyPolicyReplace
						return &p
					}(),
					AgentTemplate: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
					},
				},
			},
			wantSchedule:    "0 * * * *",
			wantConcurrency: batchv1.ReplaceConcurrent,
			wantErrNil:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewCronAgentTranslator(&mockAgentTranslator{})

			cronJob, err := translator.TranslateToCronJob(tt.cronAgent)
			if (err == nil) != tt.wantErrNil {
				t.Errorf("TranslateToCronJob() error = %v, wantErrNil %v", err, tt.wantErrNil)
				return
			}

			if err != nil {
				return
			}

			// Verify CronJob metadata
			if cronJob.Name != tt.cronAgent.Name {
				t.Errorf("CronJob name = %v, want %v", cronJob.Name, tt.cronAgent.Name)
			}

			if cronJob.Namespace != tt.cronAgent.Namespace {
				t.Errorf("CronJob namespace = %v, want %v", cronJob.Namespace, tt.cronAgent.Namespace)
			}

			// Verify owner reference
			if len(cronJob.OwnerReferences) != 1 {
				t.Errorf("expected 1 owner reference, got %d", len(cronJob.OwnerReferences))
			}

			// Verify CronJob spec
			if cronJob.Spec.Schedule != tt.wantSchedule {
				t.Errorf("CronJob schedule = %v, want %v", cronJob.Spec.Schedule, tt.wantSchedule)
			}

			if tt.wantTimezone != nil {
				if cronJob.Spec.TimeZone == nil {
					t.Error("expected timezone to be set, got nil")
				} else if *cronJob.Spec.TimeZone != *tt.wantTimezone {
					t.Errorf("CronJob timezone = %v, want %v", *cronJob.Spec.TimeZone, *tt.wantTimezone)
				}
			}

			if cronJob.Spec.ConcurrencyPolicy != tt.wantConcurrency {
				t.Errorf("CronJob concurrency = %v, want %v", cronJob.Spec.ConcurrencyPolicy, tt.wantConcurrency)
			}

			if tt.wantSuccessLimit != nil {
				if cronJob.Spec.SuccessfulJobsHistoryLimit == nil {
					t.Error("expected successful history limit to be set, got nil")
				} else if *cronJob.Spec.SuccessfulJobsHistoryLimit != *tt.wantSuccessLimit {
					t.Errorf("successful history limit = %v, want %v", *cronJob.Spec.SuccessfulJobsHistoryLimit, *tt.wantSuccessLimit)
				}
			}

			if tt.wantFailedLimit != nil {
				if cronJob.Spec.FailedJobsHistoryLimit == nil {
					t.Error("expected failed history limit to be set, got nil")
				} else if *cronJob.Spec.FailedJobsHistoryLimit != *tt.wantFailedLimit {
					t.Errorf("failed history limit = %v, want %v", *cronJob.Spec.FailedJobsHistoryLimit, *tt.wantFailedLimit)
				}
			}

			// Verify JobTemplate has correct labels
			if cronJob.Spec.JobTemplate.Labels["cronagent.kagent.dev/name"] != tt.cronAgent.Name {
				t.Errorf("missing or incorrect cronagent label in job template")
			}
		})
	}
}

func TestTranslateToJob(t *testing.T) {
	tests := []struct {
		name       string
		cronAgent  *v1alpha2.CronAgent
		timestamp  string
		wantName   string
		wantErrNil bool
	}{
		{
			name: "manual job creation",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:     "0 * * * *",
					InitialTask:  "test task",
					ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
					AgentTemplate: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
					},
				},
			},
			timestamp:  "1234567890",
			wantName:   "cronagent-test-cron-1234567890",
			wantErrNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewCronAgentTranslator(&mockAgentTranslator{})

			job, err := translator.TranslateToJob(tt.cronAgent, tt.timestamp)
			if (err == nil) != tt.wantErrNil {
				t.Errorf("TranslateToJob() error = %v, wantErrNil %v", err, tt.wantErrNil)
				return
			}

			if err != nil {
				return
			}

			// Verify Job metadata
			if job.Name != tt.wantName {
				t.Errorf("Job name = %v, want %v", job.Name, tt.wantName)
			}

			if job.Namespace != tt.cronAgent.Namespace {
				t.Errorf("Job namespace = %v, want %v", job.Namespace, tt.cronAgent.Namespace)
			}

			// Verify owner reference
			if len(job.OwnerReferences) != 1 {
				t.Errorf("expected 1 owner reference, got %d", len(job.OwnerReferences))
			}

			// Verify labels
			if job.Labels["cronagent.kagent.dev/name"] != tt.cronAgent.Name {
				t.Errorf("missing or incorrect cronagent label")
			}
		})
	}
}

func TestTranslateToJobTemplate_EnvironmentVariables(t *testing.T) {
	tests := []struct {
		name        string
		cronAgent   *v1alpha2.CronAgent
		wantEnvVars map[string]string
	}{
		{
			name: "PerRun thread policy",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:     "0 * * * *",
					InitialTask:  "generate report",
					ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
					AgentTemplate: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
					},
				},
			},
			wantEnvVars: map[string]string{
				"KAGENT_CRONAGENT_NAME": "test-cron",
				"KAGENT_INITIAL_TASK":   "generate report",
				"KAGENT_THREAD_POLICY":  "PerRun",
				"KAGENT_USER_ID":        "system",
			},
		},
		{
			name: "Persistent thread policy",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "monitor",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:     "0 * * * *",
					InitialTask:  "check system health",
					ThreadPolicy: v1alpha2.ThreadPolicyPersistent,
					AgentTemplate: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
					},
				},
			},
			wantEnvVars: map[string]string{
				"KAGENT_CRONAGENT_NAME": "monitor",
				"KAGENT_INITIAL_TASK":   "check system health",
				"KAGENT_THREAD_POLICY":  "Persistent",
				"KAGENT_USER_ID":        "system",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewCronAgentTranslator(&mockAgentTranslator{})

			cronJob, err := translator.TranslateToCronJob(tt.cronAgent)
			if err != nil {
				t.Fatalf("TranslateToCronJob() error = %v", err)
			}

			// Extract container from job template
			if len(cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers) == 0 {
				t.Fatal("no containers in job template")
			}

			container := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]

			// Verify environment variables
			envMap := make(map[string]string)
			for _, env := range container.Env {
				envMap[env.Name] = env.Value
			}

			for wantKey, wantValue := range tt.wantEnvVars {
				gotValue, ok := envMap[wantKey]
				if !ok {
					t.Errorf("missing environment variable %s", wantKey)
					continue
				}
				if gotValue != wantValue {
					t.Errorf("env var %s = %v, want %v", wantKey, gotValue, wantValue)
				}
			}

			// Verify existing env vars from agent are preserved
			if _, ok := envMap["EXISTING_VAR"]; !ok {
				t.Error("existing environment variable from agent was not preserved")
			}
		})
	}
}

func TestJobTemplateBackoffLimit(t *testing.T) {
	cronAgent := &v1alpha2.CronAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cron",
			Namespace: "default",
		},
		Spec: v1alpha2.CronAgentSpec{
			Schedule:     "0 * * * *",
			InitialTask:  "test task",
			ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
			AgentTemplate: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
			},
		},
	}

	translator := NewCronAgentTranslator(&mockAgentTranslator{})

	cronJob, err := translator.TranslateToCronJob(cronAgent)
	if err != nil {
		t.Fatalf("TranslateToCronJob() error = %v", err)
	}

	// Verify BackoffLimit is set to 0
	if cronJob.Spec.JobTemplate.Spec.BackoffLimit == nil {
		t.Fatal("BackoffLimit is nil, expected 0")
	}

	if *cronJob.Spec.JobTemplate.Spec.BackoffLimit != 0 {
		t.Errorf("BackoffLimit = %d, want 0", *cronJob.Spec.JobTemplate.Spec.BackoffLimit)
	}
}

func TestJobTemplateLabelsAndAnnotations(t *testing.T) {
	cronAgent := &v1alpha2.CronAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cron",
			Namespace: "default",
		},
		Spec: v1alpha2.CronAgentSpec{
			Schedule:     "0 * * * *",
			InitialTask:  "test task",
			ThreadPolicy: v1alpha2.ThreadPolicyPersistent,
			AgentTemplate: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
			},
		},
	}

	translator := NewCronAgentTranslator(&mockAgentTranslator{})

	cronJob, err := translator.TranslateToCronJob(cronAgent)
	if err != nil {
		t.Fatalf("TranslateToCronJob() error = %v", err)
	}

	// Verify JobTemplate labels
	jobTemplate := cronJob.Spec.JobTemplate
	expectedLabels := map[string]string{
		"app.kubernetes.io/name":       "cronagent",
		"app.kubernetes.io/instance":   "test-cron",
		"app.kubernetes.io/managed-by": "kagent",
		"cronagent.kagent.dev/name":    "test-cron",
	}

	for key, value := range expectedLabels {
		if jobTemplate.Labels[key] != value {
			t.Errorf("label %s = %v, want %v", key, jobTemplate.Labels[key], value)
		}
	}

	// Verify JobTemplate annotations
	expectedAnnotations := map[string]string{
		"kagent.dev/cronagent-name": "test-cron",
		"kagent.dev/thread-policy":  "Persistent",
	}

	for key, value := range expectedAnnotations {
		if jobTemplate.Annotations[key] != value {
			t.Errorf("annotation %s = %v, want %v", key, jobTemplate.Annotations[key], value)
		}
	}

	// Verify Pod labels
	podLabels := jobTemplate.Spec.Template.Labels
	for key, value := range expectedLabels {
		if podLabels[key] != value {
			t.Errorf("pod label %s = %v, want %v", key, podLabels[key], value)
		}
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}
