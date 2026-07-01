package service

import (
	"context"
	"fmt"
	"math"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
)

// TaskProgress is the data the task-progress MCP App View renders. It is the
// structuredContent returned by the show_task_progress / refresh_task_progress
// tools. The text Summary doubles as the tool's text fallback for non-UI hosts.
//
// For a Feature, Columns holds the per-column count of its child Tasks and
// Children lists those child Tasks; Subtasks is empty. For a Task, Subtasks
// holds the checklist items and Columns/Children are empty.
type TaskProgress struct {
	TaskID          int64          `json:"task_id"`
	Title           string         `json:"title"`
	Kind            string         `json:"kind"` // "feature" | "task"
	Status          db.TaskStatus  `json:"status"`
	Assignee        string         `json:"assignee,omitempty"`
	Labels          []string       `json:"labels,omitempty"`
	UserInputNeeded bool           `json:"user_input_needed"`
	Percent         int            `json:"percent"`     // 0..100 overall completion
	DoneCount       int            `json:"done_count"`  // children in done column, or subtasks done
	TotalCount      int            `json:"total_count"` // children, or subtasks
	Summary         string         `json:"summary"`
	Board           ProgressBoard  `json:"board"`
	Columns         []ColumnCount  `json:"columns,omitempty"`  // Feature: child count per board column
	Children        []ProgressItem `json:"children,omitempty"` // Feature: child Tasks
	Subtasks        []ProgressItem `json:"subtasks,omitempty"` // Task: checklist items
}

// ProgressBoard is the board context the widget needs to render columns and
// identify the terminal ("done") column.
type ProgressBoard struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	DoneColumn string   `json:"done_column"`
}

// ColumnCount is the number of a Feature's child Tasks sitting in one column.
type ColumnCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// ProgressItem is one row in the widget: a child Task (Status + column-derived
// Percent) for a Feature, or a checklist subtask (Done) for a Task.
type ProgressItem struct {
	ID      int64         `json:"id"`
	Title   string        `json:"title"`
	Status  db.TaskStatus `json:"status,omitempty"` // child Tasks only
	Percent int           `json:"percent"`          // child: column progress; subtask: 0/100
	Done    bool          `json:"done"`
}

// TaskProgress assembles the progress view for a single card (Feature or Task).
// It loads the card (with children/subtasks) and its board, then computes a
// completion percentage and the per-item breakdown. Not-found surfaces as a
// wrapped pgx.ErrNoRows (via GetTask).
func (s *TaskService) TaskProgress(ctx context.Context, id int64) (*TaskProgress, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}

	board, err := s.q.GetBoardByID(ctx, task.BoardID)
	if err != nil {
		return nil, fmt.Errorf("getting board for task %d: %w", id, err)
	}
	cols := board.Columns
	doneCol := ""
	if len(cols) > 0 {
		doneCol = cols[len(cols)-1]
	}

	p := &TaskProgress{
		TaskID:          task.ID,
		Title:           task.Title,
		Kind:            task.Kind,
		Status:          task.Status,
		Assignee:        task.Assignee,
		Labels:          task.Labels,
		UserInputNeeded: task.UserInputNeeded,
		Board: ProgressBoard{
			Key:        board.Key,
			Name:       board.Name,
			Columns:    cols,
			DoneColumn: doneCol,
		},
	}

	if task.Kind == db.KindFeature {
		// Each child bar reflects that child Task's own completion, so load each
		// child's checklist subtasks before computing the breakdown.
		for _, child := range task.Children {
			subs, err := s.ListSubtasks(ctx, child.ID)
			if err != nil {
				return nil, fmt.Errorf("listing subtasks for child task %d: %w", child.ID, err)
			}
			child.Subtasks = subs
		}
		fillFeatureProgress(p, task, cols, doneCol)
	} else {
		fillTaskProgress(p, task, cols, doneCol)
	}

	p.Summary = progressSummary(p)
	return p, nil
}

// fillFeatureProgress computes a Feature's percent as the mean completion of its
// child Tasks (each child's checklist ratio, or its column position when it has
// no checklist) and tallies children per column. With no children it falls back
// to the Feature's own column position.
func fillFeatureProgress(p *TaskProgress, task *Task, cols []string, doneCol string) {
	counts := make(map[string]int, len(cols))
	sum := 0
	for _, child := range task.Children {
		cp, _, _ := childTaskProgress(child.Subtasks, string(child.Status), cols)
		sum += cp
		counts[string(child.Status)]++
		p.Children = append(p.Children, ProgressItem{
			ID:      child.ID,
			Title:   child.Title,
			Status:  child.Status,
			Percent: cp,
			Done:    string(child.Status) == doneCol && doneCol != "",
		})
	}

	p.TotalCount = len(task.Children)
	p.DoneCount = counts[doneCol]

	p.Columns = make([]ColumnCount, 0, len(cols))
	for _, col := range cols {
		p.Columns = append(p.Columns, ColumnCount{Status: col, Count: counts[col]})
	}

	if p.TotalCount > 0 {
		p.Percent = int(math.Round(float64(sum) / float64(p.TotalCount)))
	} else {
		p.Percent = columnProgress(string(task.Status), cols)
	}
}

// fillTaskProgress computes a Task's percent from its checklist subtasks when it
// has any, otherwise from its own column position.
func fillTaskProgress(p *TaskProgress, task *Task, cols []string, doneCol string) {
	done := 0
	for _, sub := range task.Subtasks {
		pct := 0
		if sub.Done {
			pct = 100
			done++
		}
		p.Subtasks = append(p.Subtasks, ProgressItem{
			ID:      sub.ID,
			Title:   sub.Title,
			Percent: pct,
			Done:    sub.Done,
		})
	}

	p.TotalCount = len(task.Subtasks)
	p.DoneCount = done

	if p.TotalCount > 0 {
		p.Percent = int(math.Round(float64(done) / float64(p.TotalCount) * 100))
	} else {
		p.Percent = columnProgress(string(task.Status), cols)
	}
}

// childTaskProgress computes one child Task's completion for the Feature view:
// the checklist done ratio when the Task has checklist subtasks, otherwise its
// column position. It returns the percent and the done/total checklist counts.
func childTaskProgress(subs []*Subtask, status string, cols []string) (percent, done, total int) {
	total = len(subs)
	for _, sub := range subs {
		if sub.Done {
			done++
		}
	}
	if total > 0 {
		percent = int(math.Round(float64(done) / float64(total) * 100))
	} else {
		percent = columnProgress(status, cols)
	}
	return percent, done, total
}

// columnProgress maps a status onto a 0..100 position within the board's ordered
// columns: the first column is 0%, the last is 100%. A single-column board (or a
// status equal to the only column) is treated as complete. An unknown status is
// 0%.
func columnProgress(status string, cols []string) int {
	idx := -1
	for i, c := range cols {
		if c == status {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0
	}
	if len(cols) <= 1 {
		return 100
	}
	return int(math.Round(float64(idx) / float64(len(cols)-1) * 100))
}

// progressSummary builds the one-line human summary used as the tool's text
// fallback (shown to the model and to non-UI hosts).
func progressSummary(p *TaskProgress) string {
	if p.Kind == db.KindFeature {
		if p.TotalCount == 0 {
			return fmt.Sprintf("Feature %q has no child tasks yet; currently in column %q (%d%%).", p.Title, p.Status, p.Percent)
		}
		return fmt.Sprintf("Feature %q is %d%% complete — %d of %d child tasks done (in %q).",
			p.Title, p.Percent, p.DoneCount, p.TotalCount, p.Board.DoneColumn)
	}
	if p.TotalCount > 0 {
		return fmt.Sprintf("Task %q is %d%% complete — %d of %d checklist items done.",
			p.Title, p.Percent, p.DoneCount, p.TotalCount)
	}
	return fmt.Sprintf("Task %q is in column %q (%d%%).", p.Title, p.Status, p.Percent)
}
