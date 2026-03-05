package controller

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	fakedb "github.com/kagent-dev/kagent/go/core/internal/database/fake"
)

func TestStoreCronAgentConfig(t *testing.T) {
	tests := []struct {
		name       string
		cronAgent  *v1alpha2.CronAgent
		wantType   database.AgentType
		wantPolicy database.ThreadPolicy
	}{
		{
			name: "PerRun thread policy (default)",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:    "0 * * * *",
					InitialTask: "test task",
				},
			},
			wantType:   database.AgentTypeCronAgent,
			wantPolicy: database.ThreadPolicyPerRun,
		},
		{
			name: "Persistent thread policy",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					Schedule:     "0 * * * *",
					InitialTask:  "test task",
					ThreadPolicy: v1alpha2.ThreadPolicyPersistent,
				},
			},
			wantType:   database.AgentTypeCronAgent,
			wantPolicy: database.ThreadPolicyPersistent,
		},
		{
			name: "with all optional fields",
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
					SuccessfulJobsHistoryLimit: int32Ptr(7),
					FailedJobsHistoryLimit:     int32Ptr(3),
				},
			},
			wantType:   database.AgentTypeCronAgent,
			wantPolicy: database.ThreadPolicyPerRun,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeDB := fakedb.NewClient()
			r := &CronAgentReconciler{
				DBClient: fakeDB,
			}

			err := r.storeCronAgentConfig(context.Background(), tt.cronAgent)
			if err != nil {
				t.Fatalf("storeCronAgentConfig() error = %v", err)
			}

			// Verify the agent was stored correctly
			stored, err := fakeDB.GetCronAgent(tt.cronAgent.Name)
			if err != nil {
				t.Fatalf("GetCronAgent() error = %v", err)
			}

			if stored.Type != tt.wantType {
				t.Errorf("stored agent type = %v, want %v", stored.Type, tt.wantType)
			}

			if stored.CronAgentConfig == nil {
				t.Fatal("CronAgentConfig is nil")
			}

			if stored.CronAgentConfig.ThreadPolicy != tt.wantPolicy {
				t.Errorf("thread policy = %v, want %v", stored.CronAgentConfig.ThreadPolicy, tt.wantPolicy)
			}

			if stored.CronAgentConfig.Schedule != tt.cronAgent.Spec.Schedule {
				t.Errorf("schedule = %v, want %v", stored.CronAgentConfig.Schedule, tt.cronAgent.Spec.Schedule)
			}

			if stored.CronAgentConfig.InitialTask != tt.cronAgent.Spec.InitialTask {
				t.Errorf("initial task = %v, want %v", stored.CronAgentConfig.InitialTask, tt.cronAgent.Spec.InitialTask)
			}
		})
	}
}

func TestCreateOrUpdateCronJob(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	tests := []struct {
		name          string
		existing      *batchv1.CronJob
		cronAgent     *v1alpha2.CronAgent
		cronJob       *batchv1.CronJob
		wantCreate    bool
		wantUpdate    bool
		wantErrNil    bool
	}{
		{
			name:     "create new cronjob",
			existing: nil,
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
			},
			cronJob: &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: batchv1.CronJobSpec{
					Schedule: "0 * * * *",
				},
			},
			wantCreate: true,
			wantUpdate: false,
			wantErrNil: true,
		},
		{
			name: "update existing cronjob",
			existing: &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: batchv1.CronJobSpec{
					Schedule: "0 * * * *",
				},
			},
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
			},
			cronJob: &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: batchv1.CronJobSpec{
					Schedule: "0 9 * * *",
				},
			},
			wantCreate: false,
			wantUpdate: true,
			wantErrNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []client.Object
			if tt.existing != nil {
				objs = append(objs, tt.existing)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			r := &CronAgentReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			err := r.createOrUpdateCronJob(context.Background(), tt.cronAgent, tt.cronJob)
			if (err == nil) != tt.wantErrNil {
				t.Errorf("createOrUpdateCronJob() error = %v, wantErrNil %v", err, tt.wantErrNil)
				return
			}

			// Verify the cronjob exists with correct spec
			result := &batchv1.CronJob{}
			err = fakeClient.Get(context.Background(), types.NamespacedName{
				Name:      tt.cronJob.Name,
				Namespace: tt.cronJob.Namespace,
			}, result)

			if err != nil {
				t.Fatalf("failed to get cronjob: %v", err)
			}

			if result.Spec.Schedule != tt.cronJob.Spec.Schedule {
				t.Errorf("cronjob schedule = %v, want %v", result.Spec.Schedule, tt.cronJob.Spec.Schedule)
			}
		})
	}
}

func TestCleanupOldJobs(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	now := time.Now()
	oneHourAgo := metav1.NewTime(now.Add(-1 * time.Hour))
	twoHoursAgo := metav1.NewTime(now.Add(-2 * time.Hour))
	threeHoursAgo := metav1.NewTime(now.Add(-3 * time.Hour))
	fourHoursAgo := metav1.NewTime(now.Add(-4 * time.Hour))

	tests := []struct {
		name              string
		cronAgent         *v1alpha2.CronAgent
		existingJobs      []batchv1.Job
		wantDeletedCount  int
		wantRemainingJobs []string
	}{
		{
			name: "cleanup successful jobs beyond limit",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					SuccessfulJobsHistoryLimit: int32Ptr(2),
					FailedJobsHistoryLimit:     int32Ptr(1),
				},
			},
			existingJobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "job-1",
						Namespace:         "default",
						CreationTimestamp: oneHourAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "job-2",
						Namespace:         "default",
						CreationTimestamp: twoHoursAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "job-3",
						Namespace:         "default",
						CreationTimestamp: threeHoursAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				},
			},
			wantDeletedCount:  1,
			wantRemainingJobs: []string{"job-1", "job-2"},
		},
		{
			name: "cleanup failed jobs beyond limit",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					SuccessfulJobsHistoryLimit: int32Ptr(2),
					FailedJobsHistoryLimit:     int32Ptr(1),
				},
			},
			existingJobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "failed-job-1",
						Namespace:         "default",
						CreationTimestamp: oneHourAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Failed: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "failed-job-2",
						Namespace:         "default",
						CreationTimestamp: twoHoursAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Failed: 1,
					},
				},
			},
			wantDeletedCount:  1,
			wantRemainingJobs: []string{"failed-job-1"},
		},
		{
			name: "keep active jobs",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					SuccessfulJobsHistoryLimit: int32Ptr(1),
					FailedJobsHistoryLimit:     int32Ptr(1),
				},
			},
			existingJobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "active-job",
						Namespace:         "default",
						CreationTimestamp: oneHourAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Active: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "success-job-1",
						Namespace:         "default",
						CreationTimestamp: twoHoursAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "success-job-2",
						Namespace:         "default",
						CreationTimestamp: threeHoursAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				},
			},
			wantDeletedCount:  1,
			wantRemainingJobs: []string{"active-job", "success-job-1"},
		},
		{
			name: "default limits",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
				Spec: v1alpha2.CronAgentSpec{
					// No limits specified, should use defaults (3 success, 1 failed)
				},
			},
			existingJobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "job-1",
						Namespace:         "default",
						CreationTimestamp: oneHourAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "job-2",
						Namespace:         "default",
						CreationTimestamp: twoHoursAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "job-3",
						Namespace:         "default",
						CreationTimestamp: threeHoursAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "job-4",
						Namespace:         "default",
						CreationTimestamp: fourHoursAgo,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				},
			},
			wantDeletedCount:  1,
			wantRemainingJobs: []string{"job-1", "job-2", "job-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []client.Object
			for i := range tt.existingJobs {
				objs = append(objs, &tt.existingJobs[i])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			r := &CronAgentReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			err := r.cleanupOldJobs(context.Background(), tt.cronAgent)
			if err != nil {
				t.Fatalf("cleanupOldJobs() error = %v", err)
			}

			// List remaining jobs
			jobList := &batchv1.JobList{}
			err = fakeClient.List(context.Background(), jobList,
				client.InNamespace(tt.cronAgent.Namespace),
				client.MatchingLabels{"cronagent.kagent.dev/name": tt.cronAgent.Name},
			)
			if err != nil {
				t.Fatalf("failed to list jobs: %v", err)
			}

			if len(jobList.Items) != len(tt.wantRemainingJobs) {
				t.Errorf("remaining jobs count = %d, want %d", len(jobList.Items), len(tt.wantRemainingJobs))
			}

			// Verify correct jobs remain
			remainingNames := make(map[string]bool)
			for _, job := range jobList.Items {
				remainingNames[job.Name] = true
			}

			for _, wantName := range tt.wantRemainingJobs {
				if !remainingNames[wantName] {
					t.Errorf("expected job %s to remain, but it was deleted", wantName)
				}
			}
		})
	}
}

func TestUpdateStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1alpha2.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	now := metav1.Now()
	earlier := metav1.NewTime(now.Add(-1 * time.Hour))

	tests := []struct {
		name         string
		cronAgent    *v1alpha2.CronAgent
		existingJobs []batchv1.Job
		wantActive   int
		wantSuccess  bool
		wantFailed   bool
	}{
		{
			name: "active job",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
			},
			existingJobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "active-job",
						Namespace:         "default",
						CreationTimestamp: now,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Active:    1,
						StartTime: &now,
					},
				},
			},
			wantActive:  1,
			wantSuccess: false,
			wantFailed:  false,
		},
		{
			name: "successful job",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
			},
			existingJobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "success-job",
						Namespace:         "default",
						CreationTimestamp: earlier,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded:      1,
						StartTime:      &earlier,
						CompletionTime: &now,
					},
				},
			},
			wantActive:  0,
			wantSuccess: true,
			wantFailed:  false,
		},
		{
			name: "failed job",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
			},
			existingJobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "failed-job",
						Namespace:         "default",
						CreationTimestamp: earlier,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Failed:         1,
						StartTime:      &earlier,
						CompletionTime: &now,
					},
				},
			},
			wantActive:  0,
			wantSuccess: false,
			wantFailed:  true,
		},
		{
			name: "mixed jobs - should track latest",
			cronAgent: &v1alpha2.CronAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron",
					Namespace: "default",
				},
			},
			existingJobs: []batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "success-job-latest",
						Namespace:         "default",
						CreationTimestamp: now,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded:      1,
						StartTime:      &now,
						CompletionTime: &now,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "success-job-old",
						Namespace:         "default",
						CreationTimestamp: earlier,
						Labels: map[string]string{
							"cronagent.kagent.dev/name": "test-cron",
						},
					},
					Status: batchv1.JobStatus{
						Succeeded:      1,
						StartTime:      &earlier,
						CompletionTime: &earlier,
					},
				},
			},
			wantActive:  0,
			wantSuccess: true,
			wantFailed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []client.Object
			objs = append(objs, tt.cronAgent)
			for i := range tt.existingJobs {
				objs = append(objs, &tt.existingJobs[i])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				WithStatusSubresource(&v1alpha2.CronAgent{}).
				Build()

			r := &CronAgentReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			err := r.updateStatus(context.Background(), tt.cronAgent)
			if err != nil {
				t.Fatalf("updateStatus() error = %v", err)
			}

			if len(tt.cronAgent.Status.ActiveRuns) != tt.wantActive {
				t.Errorf("active runs count = %d, want %d", len(tt.cronAgent.Status.ActiveRuns), tt.wantActive)
			}

			if (tt.cronAgent.Status.LastSuccessfulRun != nil) != tt.wantSuccess {
				t.Errorf("last successful run present = %v, want %v", tt.cronAgent.Status.LastSuccessfulRun != nil, tt.wantSuccess)
			}

			if (tt.cronAgent.Status.LastFailedRun != nil) != tt.wantFailed {
				t.Errorf("last failed run present = %v, want %v", tt.cronAgent.Status.LastFailedRun != nil, tt.wantFailed)
			}
		})
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}
