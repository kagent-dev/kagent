package temporal

import (
	"fmt"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// NewWorker creates a Temporal worker that polls the given task queue
// and registers all workflows and activities.
func NewWorker(temporalClient client.Client, taskQueue string, activities *Activities) (worker.Worker, error) {
	if temporalClient == nil {
		return nil, fmt.Errorf("temporal client must not be nil")
	}
	if taskQueue == "" {
		return nil, fmt.Errorf("task queue must not be empty")
	}

	w := worker.New(temporalClient, taskQueue, worker.Options{})

	w.RegisterWorkflow(AgentExecutionWorkflow)
	w.RegisterActivity(activities)

	return w, nil
}
