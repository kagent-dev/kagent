package database

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/database"
	fakedb "github.com/kagent-dev/kagent/go/core/internal/database/fake"
)

func TestStoreCronAgent(t *testing.T) {
	client := fakedb.NewClient()

	cronAgent := &database.Agent{
		ID:   "test-cron",
		Type: database.AgentTypeCronAgent,
		CronAgentConfig: &database.CronAgentConfig{
			Schedule:     "0 * * * *",
			InitialTask:  "test task",
			ThreadPolicy: database.ThreadPolicyPerRun,
		},
	}

	err := client.StoreCronAgent(cronAgent)
	if err != nil {
		t.Fatalf("StoreCronAgent() error = %v", err)
	}

	// Verify it was stored
	retrieved, err := client.GetCronAgent("test-cron")
	if err != nil {
		t.Fatalf("GetCronAgent() error = %v", err)
	}

	if retrieved.ID != cronAgent.ID {
		t.Errorf("ID = %v, want %v", retrieved.ID, cronAgent.ID)
	}

	if retrieved.Type != database.AgentTypeCronAgent {
		t.Errorf("Type = %v, want %v", retrieved.Type, database.AgentTypeCronAgent)
	}

	if retrieved.CronAgentConfig == nil {
		t.Fatal("CronAgentConfig is nil")
	}

	if retrieved.CronAgentConfig.Schedule != cronAgent.CronAgentConfig.Schedule {
		t.Errorf("Schedule = %v, want %v", retrieved.CronAgentConfig.Schedule, cronAgent.CronAgentConfig.Schedule)
	}

	if retrieved.CronAgentConfig.ThreadPolicy != cronAgent.CronAgentConfig.ThreadPolicy {
		t.Errorf("ThreadPolicy = %v, want %v", retrieved.CronAgentConfig.ThreadPolicy, cronAgent.CronAgentConfig.ThreadPolicy)
	}
}

func TestGetCronAgent(t *testing.T) {
	tests := []struct {
		name        string
		stored      *database.Agent
		lookupName  string
		wantErr     bool
		wantNotNil  bool
	}{
		{
			name: "existing cron agent",
			stored: &database.Agent{
				ID:   "existing-cron",
				Type: database.AgentTypeCronAgent,
				CronAgentConfig: &database.CronAgentConfig{
					Schedule:    "0 * * * *",
					InitialTask: "test",
				},
			},
			lookupName: "existing-cron",
			wantErr:    false,
			wantNotNil: true,
		},
		{
			name:       "non-existent cron agent",
			stored:     nil,
			lookupName: "non-existent",
			wantErr:    true,
			wantNotNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fakedb.NewClient()

			if tt.stored != nil {
				err := client.StoreCronAgent(tt.stored)
				if err != nil {
					t.Fatalf("StoreCronAgent() error = %v", err)
				}
			}

			got, err := client.GetCronAgent(tt.lookupName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCronAgent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if (got != nil) != tt.wantNotNil {
				t.Errorf("GetCronAgent() got = %v, wantNotNil %v", got, tt.wantNotNil)
			}
		})
	}
}

func TestListCronAgents(t *testing.T) {
	client := fakedb.NewClient()

	// Store multiple agents of different types
	agents := []database.Agent{
		{
			ID:   "cron1",
			Type: database.AgentTypeCronAgent,
			CronAgentConfig: &database.CronAgentConfig{
				Schedule:    "0 * * * *",
				InitialTask: "task1",
			},
		},
		{
			ID:   "regular1",
			Type: database.AgentTypeRegular,
		},
		{
			ID:   "cron2",
			Type: database.AgentTypeCronAgent,
			CronAgentConfig: &database.CronAgentConfig{
				Schedule:    "0 9 * * *",
				InitialTask: "task2",
			},
		},
		{
			ID:   "cronrun1",
			Type: database.AgentTypeCronRun,
		},
	}

	for _, agent := range agents {
		var err error
		if agent.Type == database.AgentTypeCronAgent {
			err = client.StoreCronAgent(&agent)
		} else {
			err = client.StoreAgent(&agent)
		}
		if err != nil {
			t.Fatalf("Store agent error = %v", err)
		}
	}

	// List only CronAgents
	cronAgents, err := client.ListCronAgents()
	if err != nil {
		t.Fatalf("ListCronAgents() error = %v", err)
	}

	// Should only return the 2 CronAgent types
	if len(cronAgents) != 2 {
		t.Errorf("ListCronAgents() count = %d, want 2", len(cronAgents))
	}

	// Verify all returned agents are CronAgents
	for _, agent := range cronAgents {
		if agent.Type != database.AgentTypeCronAgent {
			t.Errorf("ListCronAgents() returned non-CronAgent type: %v", agent.Type)
		}
	}

	// Verify we got the right ones
	foundCron1 := false
	foundCron2 := false
	for _, agent := range cronAgents {
		if agent.ID == "cron1" {
			foundCron1 = true
		}
		if agent.ID == "cron2" {
			foundCron2 = true
		}
	}

	if !foundCron1 {
		t.Error("cron1 not found in ListCronAgents() result")
	}
	if !foundCron2 {
		t.Error("cron2 not found in ListCronAgents() result")
	}
}

func TestDeleteCronAgent(t *testing.T) {
	client := fakedb.NewClient()

	cronAgent := &database.Agent{
		ID:   "to-delete",
		Type: database.AgentTypeCronAgent,
		CronAgentConfig: &database.CronAgentConfig{
			Schedule:    "0 * * * *",
			InitialTask: "test",
		},
	}

	// Store the agent
	err := client.StoreCronAgent(cronAgent)
	if err != nil {
		t.Fatalf("StoreCronAgent() error = %v", err)
	}

	// Verify it exists
	_, err = client.GetCronAgent("to-delete")
	if err != nil {
		t.Fatalf("GetCronAgent() before delete error = %v", err)
	}

	// Delete it
	err = client.DeleteCronAgent("to-delete")
	if err != nil {
		t.Fatalf("DeleteCronAgent() error = %v", err)
	}

	// Verify it's gone
	_, err = client.GetCronAgent("to-delete")
	if err == nil {
		t.Error("GetCronAgent() after delete should return error, got nil")
	}
}

func TestListCronAgentRuns(t *testing.T) {
	client := fakedb.NewClient()

	// Store a CronAgent and several runs
	cronAgent := &database.Agent{
		ID:   "my-cron",
		Type: database.AgentTypeCronAgent,
		CronAgentConfig: &database.CronAgentConfig{
			Schedule:    "0 * * * *",
			InitialTask: "test",
		},
	}

	err := client.StoreCronAgent(cronAgent)
	if err != nil {
		t.Fatalf("StoreCronAgent() error = %v", err)
	}

	// Store runs for this CronAgent
	runs := []database.Agent{
		{
			ID:   "cronagent-my-cron-1234567890",
			Type: database.AgentTypeCronRun,
		},
		{
			ID:   "cronagent-my-cron-1234567891",
			Type: database.AgentTypeCronRun,
		},
		{
			ID:   "cronagent-my-cron-1234567892",
			Type: database.AgentTypeCronRun,
		},
		{
			// Different CronAgent's run, should not be included
			ID:   "cronagent-other-cron-1234567890",
			Type: database.AgentTypeCronRun,
		},
	}

	for _, run := range runs {
		err := client.StoreAgent(&run)
		if err != nil {
			t.Fatalf("StoreAgent() error = %v", err)
		}
	}

	// List runs for my-cron
	results, err := client.ListCronAgentRuns("my-cron", 10)
	if err != nil {
		t.Fatalf("ListCronAgentRuns() error = %v", err)
	}

	// Should return 3 runs (not the "other-cron" run)
	if len(results) != 3 {
		t.Errorf("ListCronAgentRuns() count = %d, want 3", len(results))
	}

	// Verify all returned runs belong to my-cron
	for _, run := range results {
		if run.Type != database.AgentTypeCronRun {
			t.Errorf("ListCronAgentRuns() returned non-CronRun type: %v", run.Type)
		}
		// Verify ID starts with "cronagent-my-cron-"
		expectedPrefix := "cronagent-my-cron-"
		if len(run.ID) < len(expectedPrefix) || run.ID[:len(expectedPrefix)] != expectedPrefix {
			t.Errorf("ListCronAgentRuns() returned run with wrong ID: %v", run.ID)
		}
	}

	// Test with limit
	limitedResults, err := client.ListCronAgentRuns("my-cron", 2)
	if err != nil {
		t.Fatalf("ListCronAgentRuns() with limit error = %v", err)
	}

	if len(limitedResults) != 2 {
		t.Errorf("ListCronAgentRuns() with limit=2 count = %d, want 2", len(limitedResults))
	}
}

func TestCronAgentConfigFields(t *testing.T) {
	client := fakedb.NewClient()

	timezone := "America/New_York"
	concurrencyPolicy := database.ConcurrencyPolicyForbid
	startingDeadline := int64(300)
	successLimit := int32(7)
	failedLimit := int32(3)
	suspend := true

	cronAgent := &database.Agent{
		ID:   "full-config",
		Type: database.AgentTypeCronAgent,
		CronAgentConfig: &database.CronAgentConfig{
			Schedule:                   "0 9 * * 1-5",
			Timezone:                   &timezone,
			InitialTask:                "daily report",
			ThreadPolicy:               database.ThreadPolicyPerRun,
			ConcurrencyPolicy:          &concurrencyPolicy,
			StartingDeadlineSeconds:    &startingDeadline,
			SuccessfulJobsHistoryLimit: &successLimit,
			FailedJobsHistoryLimit:     &failedLimit,
			Suspend:                    &suspend,
		},
	}

	err := client.StoreCronAgent(cronAgent)
	if err != nil {
		t.Fatalf("StoreCronAgent() error = %v", err)
	}

	retrieved, err := client.GetCronAgent("full-config")
	if err != nil {
		t.Fatalf("GetCronAgent() error = %v", err)
	}

	cfg := retrieved.CronAgentConfig
	if cfg == nil {
		t.Fatal("CronAgentConfig is nil")
	}

	// Verify all fields
	if cfg.Schedule != "0 9 * * 1-5" {
		t.Errorf("Schedule = %v, want %v", cfg.Schedule, "0 9 * * 1-5")
	}

	if cfg.Timezone == nil || *cfg.Timezone != timezone {
		t.Errorf("Timezone = %v, want %v", cfg.Timezone, timezone)
	}

	if cfg.InitialTask != "daily report" {
		t.Errorf("InitialTask = %v, want %v", cfg.InitialTask, "daily report")
	}

	if cfg.ThreadPolicy != database.ThreadPolicyPerRun {
		t.Errorf("ThreadPolicy = %v, want %v", cfg.ThreadPolicy, database.ThreadPolicyPerRun)
	}

	if cfg.ConcurrencyPolicy == nil || *cfg.ConcurrencyPolicy != concurrencyPolicy {
		t.Errorf("ConcurrencyPolicy = %v, want %v", cfg.ConcurrencyPolicy, concurrencyPolicy)
	}

	if cfg.StartingDeadlineSeconds == nil || *cfg.StartingDeadlineSeconds != startingDeadline {
		t.Errorf("StartingDeadlineSeconds = %v, want %v", cfg.StartingDeadlineSeconds, startingDeadline)
	}

	if cfg.SuccessfulJobsHistoryLimit == nil || *cfg.SuccessfulJobsHistoryLimit != successLimit {
		t.Errorf("SuccessfulJobsHistoryLimit = %v, want %v", cfg.SuccessfulJobsHistoryLimit, successLimit)
	}

	if cfg.FailedJobsHistoryLimit == nil || *cfg.FailedJobsHistoryLimit != failedLimit {
		t.Errorf("FailedJobsHistoryLimit = %v, want %v", cfg.FailedJobsHistoryLimit, failedLimit)
	}

	if cfg.Suspend == nil || *cfg.Suspend != suspend {
		t.Errorf("Suspend = %v, want %v", cfg.Suspend, suspend)
	}
}
