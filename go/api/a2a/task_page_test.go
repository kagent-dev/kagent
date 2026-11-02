package a2a

import (
	"encoding/json"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

func TestTasksWithinSize(t *testing.T) {
	big := func(n int) *a2atype.Task {
		return &a2atype.Task{ID: "t", Artifacts: []*a2atype.Artifact{{Parts: a2atype.ContentParts{a2atype.NewTextPart(strings.Repeat("x", n))}}}}
	}
	tasks := []*a2atype.Task{big(400), big(400), big(400)}
	encoded, _ := json.Marshal(tasks[0])
	one := len(encoded)
	for _, tc := range []struct{ max, want int }{{3 * one, 3}, {2 * one, 2}, {one, 1}, {1, 1}} {
		if got := TasksWithinSize(tasks, tc.max); got != tc.want {
			t.Errorf("max %d: got %d tasks, want %d", tc.max, got, tc.want)
		}
	}
}
