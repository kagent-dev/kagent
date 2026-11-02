package a2a

import (
	"encoding/json"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// MaxTaskPageBytes keeps a ListTasks page under the 16 MiB gRPC message limit.
const MaxTaskPageBytes = 12 << 20

// TasksWithinSize returns how many leading tasks fit in maxBytes of JSON. It
// returns at least one, so a single oversized task still gets a page of its own.
func TasksWithinSize(tasks []*a2atype.Task, maxBytes int) int {
	total := 0
	for i, task := range tasks {
		encoded, err := json.Marshal(task)
		if err != nil {
			return len(tasks)
		}
		total += len(encoded)
		if total > maxBytes && i > 0 {
			return i
		}
	}
	return len(tasks)
}
