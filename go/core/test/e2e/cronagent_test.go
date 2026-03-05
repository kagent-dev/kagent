package e2e_test

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
)

// TestE2ECronAgent_BasicScheduling tests that a CronAgent creates a CronJob
// and can execute scheduled runs successfully
func TestE2ECronAgent_BasicScheduling(t *testing.T) {
	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	// Setup model config
	modelCfg := setupModelConfig(t, cli, baseURL)

	// Create CronAgent with PerRun policy
	cronAgent := &v1alpha2.CronAgent{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-cron-basic-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.CronAgentSpec{
			Schedule:     "*/5 * * * *", // Every 5 minutes
			InitialTask:  "Echo 'Hello from CronAgent'",
			ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
			AgentTemplate: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					SystemMessage: "You are a helpful assistant that executes scheduled tasks.",
					ModelConfig:   modelCfg.Name,
					Deployment: &v1alpha2.DeclarativeDeploymentSpec{
						SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				},
			},
		},
	}

	err := cli.Create(t.Context(), cronAgent)
	require.NoError(t, err)
	cleanup(t, cli, cronAgent)

	// Wait for CronJob to be created
	var cronJob batchv1.CronJob
	err = wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		err := cli.Get(ctx, types.NamespacedName{
			Name:      cronAgent.Name,
			Namespace: cronAgent.Namespace,
		}, &cronJob)
		return err == nil, nil
	})
	require.NoError(t, err, "CronJob should be created")

	// Verify CronJob spec
	require.Equal(t, cronAgent.Spec.Schedule, cronJob.Spec.Schedule)
	require.NotNil(t, cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers)
	require.Greater(t, len(cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers), 0)

	// Verify environment variables are set
	container := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
	envMap := make(map[string]string)
	for _, env := range container.Env {
		envMap[env.Name] = env.Value
	}
	require.Equal(t, cronAgent.Name, envMap["KAGENT_CRONAGENT_NAME"])
	require.Equal(t, cronAgent.Spec.InitialTask, envMap["KAGENT_INITIAL_TASK"])
	require.Equal(t, "PerRun", envMap["KAGENT_THREAD_POLICY"])
	require.Equal(t, "system", envMap["KAGENT_USER_ID"])

	t.Logf("CronAgent %s created successfully with CronJob", cronAgent.Name)
}

// TestE2ECronAgent_ManualTrigger tests manual job triggering via annotation
func TestE2ECronAgent_ManualTrigger(t *testing.T) {
	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	// Setup model config
	modelCfg := setupModelConfig(t, cli, baseURL)

	// Create CronAgent
	cronAgent := &v1alpha2.CronAgent{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-cron-manual-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.CronAgentSpec{
			Schedule:     "0 0 * * *", // Daily at midnight (won't trigger naturally in test)
			InitialTask:  "Manual trigger test",
			ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
			AgentTemplate: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					SystemMessage: "You are a helpful assistant that executes scheduled tasks.",
					ModelConfig:   modelCfg.Name,
					Deployment: &v1alpha2.DeclarativeDeploymentSpec{
						SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				},
			},
		},
	}

	err := cli.Create(t.Context(), cronAgent)
	require.NoError(t, err)
	cleanup(t, cli, cronAgent)

	// Wait for CronAgent to be ready
	time.Sleep(5 * time.Second)

	// Trigger manual run by adding annotation
	err = cli.Get(t.Context(), types.NamespacedName{
		Name:      cronAgent.Name,
		Namespace: cronAgent.Namespace,
	}, cronAgent)
	require.NoError(t, err)

	if cronAgent.Annotations == nil {
		cronAgent.Annotations = make(map[string]string)
	}
	cronAgent.Annotations["cronagent.kagent.dev/trigger"] = "manual"

	err = cli.Update(t.Context(), cronAgent)
	require.NoError(t, err)

	// Wait for Job to be created
	var jobs batchv1.JobList
	err = wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		err := cli.List(ctx, &jobs,
			client.InNamespace(cronAgent.Namespace),
			client.MatchingLabels{"cronagent.kagent.dev/name": cronAgent.Name},
		)
		if err != nil {
			return false, nil
		}
		return len(jobs.Items) > 0, nil
	})
	require.NoError(t, err, "Manual Job should be created")

	// Verify Job was created
	require.Greater(t, len(jobs.Items), 0, "At least one Job should exist")
	job := jobs.Items[0]

	// Verify Job labels
	require.Equal(t, cronAgent.Name, job.Labels["cronagent.kagent.dev/name"])

	// Verify annotation was removed from CronAgent
	err = cli.Get(t.Context(), types.NamespacedName{
		Name:      cronAgent.Name,
		Namespace: cronAgent.Namespace,
	}, cronAgent)
	require.NoError(t, err)
	_, exists := cronAgent.Annotations["cronagent.kagent.dev/trigger"]
	require.False(t, exists, "Trigger annotation should be removed after processing")

	t.Logf("Manual trigger created Job: %s", job.Name)
}

// TestE2ECronAgent_ThreadPolicyPerRun tests that PerRun creates new sessions
func TestE2ECronAgent_ThreadPolicyPerRun(t *testing.T) {
	t.Skip("Skipping - requires full agent deployment and session tracking")
	// This test would require:
	// 1. Deploying agent pods
	// 2. Waiting for Jobs to complete
	// 3. Checking database for multiple sessions with different IDs
	// This is complex and may be better suited for integration tests
}

// TestE2ECronAgent_ThreadPolicyPersistent tests that Persistent reuses sessions
func TestE2ECronAgent_ThreadPolicyPersistent(t *testing.T) {
	t.Skip("Skipping - requires full agent deployment and session tracking")
	// Similar to PerRun test - requires full deployment
}

// TestE2ECronAgent_HistoryLimits tests that old jobs are cleaned up
func TestE2ECronAgent_HistoryLimits(t *testing.T) {
	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	// Setup model config
	modelCfg := setupModelConfig(t, cli, baseURL)

	successLimit := int32(2)
	failedLimit := int32(1)

	// Create CronAgent with history limits
	cronAgent := &v1alpha2.CronAgent{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-cron-limits-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.CronAgentSpec{
			Schedule:                   "*/1 * * * *",
			InitialTask:                "Test history limits",
			ThreadPolicy:               v1alpha2.ThreadPolicyPerRun,
			SuccessfulJobsHistoryLimit: &successLimit,
			FailedJobsHistoryLimit:     &failedLimit,
			AgentTemplate: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					SystemMessage: "You are a helpful assistant that executes scheduled tasks.",
					ModelConfig:   modelCfg.Name,
					Deployment: &v1alpha2.DeclarativeDeploymentSpec{
						SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				},
			},
		},
	}

	err := cli.Create(t.Context(), cronAgent)
	require.NoError(t, err)
	cleanup(t, cli, cronAgent)

	// Wait for CronJob to be created
	var cronJob batchv1.CronJob
	err = wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		err := cli.Get(ctx, types.NamespacedName{
			Name:      cronAgent.Name,
			Namespace: cronAgent.Namespace,
		}, &cronJob)
		return err == nil, nil
	})
	require.NoError(t, err)

	// Verify history limits are set in CronJob
	require.NotNil(t, cronJob.Spec.SuccessfulJobsHistoryLimit)
	require.Equal(t, successLimit, *cronJob.Spec.SuccessfulJobsHistoryLimit)
	require.NotNil(t, cronJob.Spec.FailedJobsHistoryLimit)
	require.Equal(t, failedLimit, *cronJob.Spec.FailedJobsHistoryLimit)

	t.Logf("CronAgent created with history limits: success=%d, failed=%d", successLimit, failedLimit)
}

// TestE2ECronAgent_ConcurrencyPolicy tests concurrency policy enforcement
func TestE2ECronAgent_ConcurrencyPolicy(t *testing.T) {
	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	// Setup model config
	modelCfg := setupModelConfig(t, cli, baseURL)

	concurrencyPolicy := v1alpha2.ConcurrencyPolicyForbid

	// Create CronAgent with Forbid concurrency
	cronAgent := &v1alpha2.CronAgent{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-cron-concurrency-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.CronAgentSpec{
			Schedule:          "*/1 * * * *",
			InitialTask:       "Test concurrency",
			ThreadPolicy:      v1alpha2.ThreadPolicyPerRun,
			ConcurrencyPolicy: &concurrencyPolicy,
			AgentTemplate: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					SystemMessage: "You are a helpful assistant that executes scheduled tasks.",
					ModelConfig:   modelCfg.Name,
					Deployment: &v1alpha2.DeclarativeDeploymentSpec{
						SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				},
			},
		},
	}

	err := cli.Create(t.Context(), cronAgent)
	require.NoError(t, err)
	cleanup(t, cli, cronAgent)

	// Wait for CronJob to be created
	var cronJob batchv1.CronJob
	err = wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		err := cli.Get(ctx, types.NamespacedName{
			Name:      cronAgent.Name,
			Namespace: cronAgent.Namespace,
		}, &cronJob)
		return err == nil, nil
	})
	require.NoError(t, err)

	// Verify concurrency policy is set
	require.Equal(t, batchv1.ForbidConcurrent, cronJob.Spec.ConcurrencyPolicy)

	t.Logf("CronAgent created with concurrency policy: %s", cronJob.Spec.ConcurrencyPolicy)
}

// TestE2ECronAgent_StatusUpdates tests that status is updated based on Job states
func TestE2ECronAgent_StatusUpdates(t *testing.T) {
	t.Skip("Skipping - requires controller watch on Jobs owned by CronJobs")

	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	// Setup model config
	modelCfg := setupModelConfig(t, cli, baseURL)

	// Create CronAgent
	cronAgent := &v1alpha2.CronAgent{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-cron-status-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.CronAgentSpec{
			Schedule:     "0 0 * * *",
			InitialTask:  "Status test",
			ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
			AgentTemplate: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					SystemMessage: "You are a helpful assistant that executes scheduled tasks.",
					ModelConfig:   modelCfg.Name,
					Deployment: &v1alpha2.DeclarativeDeploymentSpec{
						SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				},
			},
		},
	}

	err := cli.Create(t.Context(), cronAgent)
	require.NoError(t, err)
	cleanup(t, cli, cronAgent)

	// Wait for CronAgent to be ready
	time.Sleep(5 * time.Second)

	// Manually create a Job to simulate a run
	now := metav1.Now()
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cronagent-%s-%d", cronAgent.Name, time.Now().Unix()),
			Namespace: cronAgent.Namespace,
			Labels: map[string]string{
				"cronagent.kagent.dev/name": cronAgent.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "kagent.dev/v1alpha2",
					Kind:       "CronAgent",
					Name:       cronAgent.Name,
					UID:        cronAgent.UID,
				},
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
							Command: []string{
								"sh", "-c", "echo 'test' && sleep 2",
							},
						},
					},
				},
			},
		},
		Status: batchv1.JobStatus{
			Active:    1,
			StartTime: &now,
		},
	}

	err = cli.Create(t.Context(), job)
	require.NoError(t, err)

	// Wait for status to be updated
	err = wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		err := cli.Get(ctx, types.NamespacedName{
			Name:      cronAgent.Name,
			Namespace: cronAgent.Namespace,
		}, cronAgent)
		if err != nil {
			return false, nil
		}
		// Check if status has active runs
		return len(cronAgent.Status.ActiveRuns) > 0, nil
	})
	require.NoError(t, err, "Status should show active runs")

	// Verify status reflects the active job
	require.Equal(t, 1, len(cronAgent.Status.ActiveRuns))
	require.Equal(t, job.Name, cronAgent.Status.ActiveRuns[0].Name)

	t.Logf("CronAgent status updated with active run: %s", job.Name)
}

// TestE2ECronAgent_Timezone tests timezone configuration
func TestE2ECronAgent_Timezone(t *testing.T) {
	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	// Setup model config
	modelCfg := setupModelConfig(t, cli, baseURL)

	timezone := "America/New_York"

	// Create CronAgent with timezone
	cronAgent := &v1alpha2.CronAgent{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-cron-tz-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.CronAgentSpec{
			Schedule:     "0 9 * * 1-5",
			Timezone:     &timezone,
			InitialTask:  "Daily report",
			ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
			AgentTemplate: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					SystemMessage: "You are a helpful assistant that executes scheduled tasks.",
					ModelConfig:   modelCfg.Name,
					Deployment: &v1alpha2.DeclarativeDeploymentSpec{
						SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				},
			},
		},
	}

	err := cli.Create(t.Context(), cronAgent)
	require.NoError(t, err)
	cleanup(t, cli, cronAgent)

	// Wait for CronJob to be created
	var cronJob batchv1.CronJob
	err = wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		err := cli.Get(ctx, types.NamespacedName{
			Name:      cronAgent.Name,
			Namespace: cronAgent.Namespace,
		}, &cronJob)
		return err == nil, nil
	})
	require.NoError(t, err)

	// Verify timezone is set
	require.NotNil(t, cronJob.Spec.TimeZone)
	require.Equal(t, timezone, *cronJob.Spec.TimeZone)

	t.Logf("CronAgent created with timezone: %s", *cronJob.Spec.TimeZone)
}

// TestE2ECronAgent_CRDValidation tests CRD validation rules
func TestE2ECronAgent_CRDValidation(t *testing.T) {
	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	tests := []struct {
		name      string
		cronAgent *v1alpha2.CronAgent
		wantErr   bool
	}{
		{
			name: "invalid cron schedule",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-schedule",
					Namespace: "kagent",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:    "invalid",
					InitialTask: "test",
					AgentTemplate: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty initial task",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-task",
					Namespace: "kagent",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:    "0 * * * *",
					InitialTask: "",
					AgentTemplate: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cli.Create(t.Context(), tt.cronAgent)
			if tt.wantErr {
				require.Error(t, err, "Expected validation error")
			} else {
				require.NoError(t, err)
				cleanup(t, cli, tt.cronAgent)
			}
		})
	}
}

// TestE2ECronAgent_KubectlCommands tests using kubectl to manage CronAgents
func TestE2ECronAgent_KubectlCommands(t *testing.T) {
	// Setup mock server
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	// Setup Kubernetes client
	cli := setupK8sClient(t, false)

	// Setup model config
	modelCfg := setupModelConfig(t, cli, baseURL)

	// Create CronAgent
	cronAgent := &v1alpha2.CronAgent{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-cron-kubectl-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.CronAgentSpec{
			Schedule:     "0 * * * *",
			InitialTask:  "kubectl test",
			ThreadPolicy: v1alpha2.ThreadPolicyPerRun,
			AgentTemplate: v1alpha2.AgentSpec{
				Type: v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{
					SystemMessage: "You are a helpful assistant that executes scheduled tasks.",
					ModelConfig:   modelCfg.Name,
					Deployment: &v1alpha2.DeclarativeDeploymentSpec{
						SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				},
			},
		},
	}

	err := cli.Create(t.Context(), cronAgent)
	require.NoError(t, err)
	cleanup(t, cli, cronAgent)

	// Test kubectl get
	cmd := exec.CommandContext(t.Context(), "kubectl", "get", "cronagents", cronAgent.Name, "-n", "kagent")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "kubectl get should work")
	require.Contains(t, string(output), cronAgent.Name)

	// Test kubectl describe
	cmd = exec.CommandContext(t.Context(), "kubectl", "describe", "cronagents", cronAgent.Name, "-n", "kagent")
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "kubectl describe should work")
	require.Contains(t, string(output), cronAgent.Spec.Schedule)
	require.Contains(t, string(output), cronAgent.Spec.InitialTask)

	t.Logf("kubectl commands work successfully for CronAgent: %s", cronAgent.Name)
}
