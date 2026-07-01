package service

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	testcontainers "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
	dbgen "github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db/gen"
	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/migrations"
)

// countingBroadcaster records how many times Broadcast was invoked. It is the
// test double for the SSE Hub injected in Step 6.
type countingBroadcaster struct {
	mu    sync.Mutex
	count int
	last  any
}

func (c *countingBroadcaster) Broadcast(_ string, event any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
	c.last = event
}

func (c *countingBroadcaster) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

// startPostgres starts a Postgres container, runs the kanban migrations, and
// returns a connection string. Tests skip when Docker is not available. This is
// a thin local copy of go/core/internal/dbtest (which cannot be imported across
// the internal/ boundary).
func startPostgres(ctx context.Context, t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available, skipping container test")
	}

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("kanban_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("kanban"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(context.Background()); err != nil {
			t.Logf("warning: failed to terminate postgres container: %v", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("getting connection string: %v", err)
	}

	if err := migrations.RunUp(connStr); err != nil {
		t.Fatalf("running migrations: %v", err)
	}
	return connStr
}

// newTestService starts Postgres, migrates, and returns a TaskService wired to a
// counting broadcaster plus the pool for direct verification queries.
func newTestService(ctx context.Context, t *testing.T) (*TaskService, *countingBroadcaster, *pgxpool.Pool) {
	t.Helper()
	url := startPostgres(ctx, t)

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	t.Cleanup(pool.Close)

	b := &countingBroadcaster{}
	svc := NewTaskService(dbgen.New(pool), pool, b)
	return svc, b, pool
}

// dbAssignee reads the assignee column directly from the DB, bypassing the
// service, to verify a value was actually persisted.
func dbAssignee(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64) string {
	t.Helper()
	var assignee string
	if err := pool.QueryRow(ctx, "SELECT assignee FROM kanban.task WHERE id = $1", id).Scan(&assignee); err != nil {
		t.Fatalf("querying assignee for task %d: %v", id, err)
	}
	return assignee
}

func TestCreateTask_Defaults(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	got, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "no status"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if got.Status != db.StatusInbox {
		t.Errorf("default status = %q, want %q", got.Status, db.StatusInbox)
	}
}

func TestCreateTask_WithStatus(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	got, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "planned", Status: db.StatusPlan})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if got.Status != db.StatusPlan {
		t.Errorf("status = %q, want %q", got.Status, db.StatusPlan)
	}

	// Verify persisted, not just echoed back.
	fetched, err := svc.GetTask(ctx, got.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if fetched.Status != db.StatusPlan {
		t.Errorf("persisted status = %q, want %q", fetched.Status, db.StatusPlan)
	}
}

func TestCreateTask_WithLabels(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	labels := []string{"priority:high", "team:platform"}
	got, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "labeled", Labels: labels})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if len(got.Labels) != 2 || got.Labels[0] != "priority:high" || got.Labels[1] != "team:platform" {
		t.Errorf("labels = %v, want %v", got.Labels, labels)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	_, err := svc.GetTask(ctx, 999999)
	if err == nil {
		t.Fatal("GetTask() expected error for missing task, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("GetTask() error = %v, want wrapped pgx.ErrNoRows", err)
	}
}

func TestMoveTask_Valid(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	created, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "to move"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	moved, err := svc.MoveTask(ctx, created.ID, db.StatusDevelop)
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if moved.Status != db.StatusDevelop {
		t.Errorf("status = %q, want %q", moved.Status, db.StatusDevelop)
	}

	fetched, err := svc.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if fetched.Status != db.StatusDevelop {
		t.Errorf("persisted status = %q, want %q", fetched.Status, db.StatusDevelop)
	}
}

func TestMoveTask_InvalidStatus(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	created, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "stay put"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if _, err := svc.MoveTask(ctx, created.ID, db.TaskStatus("Nonsense")); err == nil {
		t.Fatal("MoveTask() expected error for invalid status, got nil")
	}

	// Status must be unchanged in the DB (no write happened).
	fetched, err := svc.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if fetched.Status != db.StatusInbox {
		t.Errorf("status changed to %q despite invalid move; want %q", fetched.Status, db.StatusInbox)
	}
}

func TestUpdateTask_Partial(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	created, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "original", Description: "desc"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	newTitle := "updated"
	got, err := svc.UpdateTask(ctx, created.ID, UpdateTaskRequest{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if got.Title != "updated" {
		t.Errorf("title = %q, want %q", got.Title, "updated")
	}
	if got.Description != "desc" {
		t.Errorf("description = %q, want unchanged %q", got.Description, "desc")
	}
}

func TestListTasks_Filter(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	for _, tc := range []CreateTaskRequest{
		{Title: "a", Status: db.StatusInbox},
		{Title: "b", Status: db.StatusInbox},
		{Title: "c", Status: db.StatusPlan},
	} {
		if _, err := svc.CreateTask(ctx, "", tc); err != nil {
			t.Fatalf("CreateTask(%s) error = %v", tc.Title, err)
		}
	}

	got, err := svc.ListTasks(ctx, "", TaskFilter{Status: new(db.StatusInbox)})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(got) != 2 {
		t.Errorf("Inbox tasks = %d, want 2", len(got))
	}

	all, err := svc.ListTasks(ctx, "", TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all tasks = %d, want 3", len(all))
	}
}

func TestListTasks_LabelFilter(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	if _, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "backend", Labels: []string{"team:backend"}}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "frontend", Labels: []string{"team:frontend"}}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	got, err := svc.ListTasks(ctx, "", TaskFilter{Label: new("team:backend")})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("label-filtered tasks = %d, want 1", len(got))
	}
	if got[0].Title != "backend" {
		t.Errorf("filtered task = %q, want %q", got[0].Title, "backend")
	}
}

func TestDeleteTask_Simple(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	created, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "doomed"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if err := svc.DeleteTask(ctx, created.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	if _, err := svc.GetTask(ctx, created.ID); !IsNotFound(err) {
		t.Errorf("GetTask() after delete error = %v, want not-found", err)
	}
}

func TestAssignTask(t *testing.T) {
	ctx := context.Background()
	svc, _, pool := newTestService(ctx, t)

	created, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "assign me"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	// Assign.
	got, err := svc.AssignTask(ctx, created.ID, "alice")
	if err != nil {
		t.Fatalf("AssignTask() error = %v", err)
	}
	if got.Assignee != "alice" {
		t.Errorf("assignee = %q, want %q", got.Assignee, "alice")
	}
	if a := dbAssignee(ctx, t, pool, created.ID); a != "alice" {
		t.Errorf("persisted assignee = %q, want %q", a, "alice")
	}

	// Reassign.
	got, err = svc.AssignTask(ctx, created.ID, "bob")
	if err != nil {
		t.Fatalf("AssignTask() reassign error = %v", err)
	}
	if got.Assignee != "bob" {
		t.Errorf("reassigned = %q, want %q", got.Assignee, "bob")
	}

	// Clear.
	got, err = svc.AssignTask(ctx, created.ID, "")
	if err != nil {
		t.Fatalf("AssignTask() clear error = %v", err)
	}
	if got.Assignee != "" {
		t.Errorf("cleared assignee = %q, want empty", got.Assignee)
	}
	if a := dbAssignee(ctx, t, pool, created.ID); a != "" {
		t.Errorf("persisted assignee = %q, want empty", a)
	}
}

func TestListTasks_AssigneeFilter(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	alice1, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "a1"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := svc.AssignTask(ctx, alice1.ID, "alice"); err != nil {
		t.Fatalf("AssignTask() error = %v", err)
	}
	bob1, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "b1"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := svc.AssignTask(ctx, bob1.ID, "bob"); err != nil {
		t.Fatalf("AssignTask() error = %v", err)
	}

	got, err := svc.ListTasks(ctx, "", TaskFilter{Assignee: new("alice")})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("assignee-filtered tasks = %d, want 1", len(got))
	}
	if got[0].Title != "a1" {
		t.Errorf("filtered task = %q, want %q", got[0].Title, "a1")
	}
}

// childTask is a helper that creates a Feature and a child Task under it,
// returning both.
func childTask(ctx context.Context, t *testing.T, svc *TaskService) (*Task, *Task) {
	t.Helper()
	feature, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "feature"})
	if err != nil {
		t.Fatalf("CreateTask(feature) error = %v", err)
	}
	task, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "task", ParentID: &feature.ID})
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	return feature, task
}

func TestCreateChildTask_Valid(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	feature, task := childTask(ctx, t, svc)
	if feature.Kind != db.KindFeature {
		t.Errorf("feature kind = %q, want %q", feature.Kind, db.KindFeature)
	}
	if task.Kind != db.KindTask {
		t.Errorf("task kind = %q, want %q", task.Kind, db.KindTask)
	}
	if task.ParentID == nil || *task.ParentID != feature.ID {
		t.Errorf("task ParentID = %v, want %d", task.ParentID, feature.ID)
	}
	if task.Status != db.StatusInbox {
		t.Errorf("task status = %q, want %q", task.Status, db.StatusInbox)
	}
}

func TestCreateChildTask_ParentNotFound(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	missing := int64(999999)
	_, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "orphan", ParentID: &missing})
	if err == nil {
		t.Fatal("CreateTask() expected error for missing parent, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("CreateTask() error = %v, want wrapped pgx.ErrNoRows", err)
	}
}

func TestCreateChildTask_NestedRejection(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	_, task := childTask(ctx, t, svc)

	_, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "grandchild", ParentID: &task.ID})
	if err == nil {
		t.Fatal("CreateTask() expected error nesting under a Task, got nil")
	}
	if err.Error() != "a task's parent must be a feature" {
		t.Errorf("error = %q, want %q", err.Error(), "a task's parent must be a feature")
	}
}

func TestCreateSubtask_Checklist(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	feature, task := childTask(ctx, t, svc)

	sub, err := svc.CreateSubtask(ctx, task.ID, "write the code")
	if err != nil {
		t.Fatalf("CreateSubtask() error = %v", err)
	}
	if sub.TaskID != task.ID {
		t.Errorf("subtask TaskID = %d, want %d", sub.TaskID, task.ID)
	}
	if sub.Done {
		t.Error("new subtask Done = true, want false")
	}
	if sub.Title != "write the code" {
		t.Errorf("subtask title = %q, want %q", sub.Title, "write the code")
	}

	// Subtasks can only be added to Tasks, not Features.
	if _, err := svc.CreateSubtask(ctx, feature.ID, "nope"); err == nil {
		t.Fatal("CreateSubtask() on a Feature: expected error, got nil")
	}
}

func TestSubtask_ToggleUpdateDelete(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	_, task := childTask(ctx, t, svc)
	sub, err := svc.CreateSubtask(ctx, task.ID, "step 1")
	if err != nil {
		t.Fatalf("CreateSubtask() error = %v", err)
	}

	toggled, err := svc.ToggleSubtask(ctx, sub.ID, true)
	if err != nil {
		t.Fatalf("ToggleSubtask() error = %v", err)
	}
	if !toggled.Done {
		t.Error("ToggleSubtask(true) Done = false, want true")
	}

	renamed, err := svc.UpdateSubtask(ctx, sub.ID, "step one")
	if err != nil {
		t.Fatalf("UpdateSubtask() error = %v", err)
	}
	if renamed.Title != "step one" {
		t.Errorf("title = %q, want %q", renamed.Title, "step one")
	}
	if !renamed.Done {
		t.Error("UpdateSubtask cleared Done; want it preserved")
	}

	if err := svc.DeleteSubtask(ctx, sub.ID); err != nil {
		t.Fatalf("DeleteSubtask() error = %v", err)
	}
	subs, err := svc.ListSubtasks(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListSubtasks() error = %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("subtasks after delete = %d, want 0", len(subs))
	}
}

func TestDeleteTask_Cascade(t *testing.T) {
	ctx := context.Background()
	svc, _, pool := newTestService(ctx, t)

	feature, task := childTask(ctx, t, svc)
	sub, err := svc.CreateSubtask(ctx, task.ID, "checklist item")
	if err != nil {
		t.Fatalf("CreateSubtask() error = %v", err)
	}

	// Deleting the Feature cascades to its child Task and that Task's checklist.
	if err := svc.DeleteTask(ctx, feature.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}

	for _, id := range []int64{feature.ID, task.ID} {
		if _, err := svc.GetTask(ctx, id); !IsNotFound(err) {
			t.Errorf("GetTask(%d) after cascade delete error = %v, want not-found", id, err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM kanban.subtask WHERE id = $1", sub.ID).Scan(&n); err != nil {
		t.Fatalf("counting subtask: %v", err)
	}
	if n != 0 {
		t.Errorf("subtask rows after cascade = %d, want 0", n)
	}
}

func TestGetTask_WithChecklistAndChildren(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(ctx, t)

	feature, task := childTask(ctx, t, svc)
	if _, err := svc.CreateSubtask(ctx, task.ID, "item1"); err != nil {
		t.Fatalf("CreateSubtask() error = %v", err)
	}
	if _, err := svc.CreateSubtask(ctx, task.ID, "item2"); err != nil {
		t.Fatalf("CreateSubtask() error = %v", err)
	}

	// The Task carries its checklist subtasks.
	gotTask, err := svc.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask(task) error = %v", err)
	}
	if len(gotTask.Subtasks) != 2 {
		t.Fatalf("task subtasks = %d, want 2", len(gotTask.Subtasks))
	}
	if gotTask.Subtasks[0].Title != "item1" || gotTask.Subtasks[1].Title != "item2" {
		t.Errorf("subtask titles = [%q, %q], want [item1, item2]",
			gotTask.Subtasks[0].Title, gotTask.Subtasks[1].Title)
	}

	// The Feature carries its child Tasks.
	gotFeature, err := svc.GetTask(ctx, feature.ID)
	if err != nil {
		t.Fatalf("GetTask(feature) error = %v", err)
	}
	if len(gotFeature.Children) != 1 || gotFeature.Children[0].ID != task.ID {
		t.Errorf("feature children = %+v, want only task %d", gotFeature.Children, task.ID)
	}
}

func TestBroadcast_CalledOnMutation(t *testing.T) {
	ctx := context.Background()
	svc, b, _ := newTestService(ctx, t)

	tests := []struct {
		name string
		op   func() error
	}{
		{
			name: "create",
			op: func() error {
				_, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "x"})
				return err
			},
		},
		{
			name: "update",
			op: func() error {
				created, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "y"})
				if err != nil {
					return err
				}
				title := "y2"
				_, err = svc.UpdateTask(ctx, created.ID, UpdateTaskRequest{Title: &title})
				return err
			},
		},
		{
			name: "move",
			op: func() error {
				created, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "z"})
				if err != nil {
					return err
				}
				_, err = svc.MoveTask(ctx, created.ID, db.StatusPlan)
				return err
			},
		},
		{
			name: "delete",
			op: func() error {
				created, err := svc.CreateTask(ctx, "", CreateTaskRequest{Title: "w"})
				if err != nil {
					return err
				}
				return svc.DeleteTask(ctx, created.ID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := b.calls()
			if err := tt.op(); err != nil {
				t.Fatalf("%s op error = %v", tt.name, err)
			}
			if got := b.calls() - before; got < 1 {
				t.Errorf("%s: Broadcast called %d times, want >= 1", tt.name, got)
			}
		})
	}
}
