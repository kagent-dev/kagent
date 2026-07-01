package service

import (
	"testing"

	"github.com/kagent-dev/kagent/go/plugins/kanban-mcp/internal/db"
)

func TestColumnProgress(t *testing.T) {
	cols := db.DefaultColumns // Inbox..Done, 7 columns

	tests := []struct {
		name   string
		status string
		cols   []string
		want   int
	}{
		{name: "first column is 0%", status: "Inbox", cols: cols, want: 0},
		{name: "last column is 100%", status: "Done", cols: cols, want: 100},
		{name: "middle column", status: "Develop", cols: cols, want: 33},
		{name: "unknown status is 0%", status: "Nope", cols: cols, want: 0},
		{name: "single column present is 100%", status: "Only", cols: []string{"Only"}, want: 100},
		{name: "no columns is 0%", status: "x", cols: nil, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := columnProgress(tt.status, tt.cols); got != tt.want {
				t.Errorf("columnProgress(%q) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

func TestFillTaskProgress_Subtasks(t *testing.T) {
	cols := db.DefaultColumns
	task := &Task{
		ID: 1, Title: "Impl", Kind: db.KindTask, Status: db.StatusDevelop,
		Subtasks: []*Subtask{
			{ID: 1, Title: "a", Done: true},
			{ID: 2, Title: "b", Done: true},
			{ID: 3, Title: "c", Done: false},
			{ID: 4, Title: "d", Done: false},
		},
	}
	p := &TaskProgress{Kind: task.Kind, Title: task.Title, Status: task.Status}
	fillTaskProgress(p, task, cols, "Done")

	if p.TotalCount != 4 || p.DoneCount != 2 {
		t.Fatalf("counts = %d/%d, want 2/4", p.DoneCount, p.TotalCount)
	}
	if p.Percent != 50 {
		t.Errorf("percent = %d, want 50 (from checklist, not column)", p.Percent)
	}
	if len(p.Subtasks) != 4 || !p.Subtasks[0].Done || p.Subtasks[0].Percent != 100 {
		t.Errorf("subtasks not mapped correctly: %+v", p.Subtasks)
	}
}

func TestFillTaskProgress_NoSubtasks_UsesColumn(t *testing.T) {
	cols := db.DefaultColumns
	task := &Task{ID: 1, Kind: db.KindTask, Status: db.StatusDone}
	p := &TaskProgress{Kind: task.Kind, Status: task.Status}
	fillTaskProgress(p, task, cols, "Done")

	if p.Percent != 100 {
		t.Errorf("percent = %d, want 100 (Done column, no subtasks)", p.Percent)
	}
	if p.TotalCount != 0 {
		t.Errorf("total = %d, want 0", p.TotalCount)
	}
}

func TestFillFeatureProgress(t *testing.T) {
	cols := db.DefaultColumns
	feature := &Task{
		ID: 1, Title: "Epic", Kind: db.KindFeature, Status: db.StatusPlan,
		Children: []*Task{
			{ID: 2, Title: "c1", Status: db.StatusDone},  // 100
			{ID: 3, Title: "c2", Status: db.StatusDone},  // 100
			{ID: 4, Title: "c3", Status: db.StatusInbox}, // 0
		},
	}
	p := &TaskProgress{Kind: feature.Kind, Title: feature.Title, Status: feature.Status}
	fillFeatureProgress(p, feature, cols, "Done")

	if p.TotalCount != 3 || p.DoneCount != 2 {
		t.Fatalf("counts = %d/%d, want 2/3 done", p.DoneCount, p.TotalCount)
	}
	// mean(100,100,0) = 66.67 -> 67
	if p.Percent != 67 {
		t.Errorf("percent = %d, want 67 (mean child column progress)", p.Percent)
	}
	if len(p.Columns) != len(cols) {
		t.Fatalf("columns = %d, want %d (all board columns)", len(p.Columns), len(cols))
	}
	// Inbox should have 1, Done should have 2.
	counts := map[string]int{}
	for _, c := range p.Columns {
		counts[c.Status] = c.Count
	}
	if counts["Inbox"] != 1 || counts["Done"] != 2 {
		t.Errorf("column counts = %+v, want Inbox=1 Done=2", counts)
	}
	if len(p.Children) != 3 || !p.Children[0].Done {
		t.Errorf("children not mapped correctly: %+v", p.Children)
	}
}

func TestFillFeatureProgress_ChildChecklistDrivesPercent(t *testing.T) {
	cols := db.DefaultColumns
	feature := &Task{
		ID: 1, Title: "Epic", Kind: db.KindFeature, Status: db.StatusPlan,
		Children: []*Task{
			// Child in the first column but with a half-done checklist -> 50%,
			// not its column position (0%).
			{ID: 2, Title: "c1", Status: db.StatusInbox, Subtasks: []*Subtask{
				{ID: 1, Done: true}, {ID: 2, Done: false},
			}},
			// Child with no checklist falls back to its column position (Done -> 100%).
			{ID: 3, Title: "c2", Status: db.StatusDone},
		},
	}
	p := &TaskProgress{Kind: feature.Kind, Title: feature.Title, Status: feature.Status}
	fillFeatureProgress(p, feature, cols, "Done")

	if p.Children[0].Percent != 50 {
		t.Errorf("c1 percent = %d, want 50 (checklist ratio, not column)", p.Children[0].Percent)
	}
	if p.Children[1].Percent != 100 {
		t.Errorf("c2 percent = %d, want 100 (Done column, no checklist)", p.Children[1].Percent)
	}
	// mean(50,100) = 75
	if p.Percent != 75 {
		t.Errorf("percent = %d, want 75 (mean of child completions)", p.Percent)
	}
	// done_count still tracks children sitting in the done column.
	if p.DoneCount != 1 || p.TotalCount != 2 {
		t.Errorf("counts = %d/%d, want 1/2", p.DoneCount, p.TotalCount)
	}
}

func TestFillFeatureProgress_NoChildren_UsesOwnColumn(t *testing.T) {
	cols := db.DefaultColumns
	feature := &Task{ID: 1, Kind: db.KindFeature, Status: db.StatusDevelop}
	p := &TaskProgress{Kind: feature.Kind, Status: feature.Status}
	fillFeatureProgress(p, feature, cols, "Done")

	if p.TotalCount != 0 {
		t.Errorf("total = %d, want 0", p.TotalCount)
	}
	if p.Percent != 33 {
		t.Errorf("percent = %d, want 33 (own Develop column position)", p.Percent)
	}
}

func TestChildTaskProgress(t *testing.T) {
	cols := db.DefaultColumns // Inbox..Done

	tests := []struct {
		name        string
		subs        []*Subtask
		status      string
		wantPercent int
		wantDone    int
		wantTotal   int
	}{
		{
			name:        "checklist ratio wins over column",
			subs:        []*Subtask{{Done: true}, {Done: true}, {Done: false}, {Done: false}},
			status:      "Inbox",
			wantPercent: 50, wantDone: 2, wantTotal: 4,
		},
		{
			name:        "all checklist items done is 100",
			subs:        []*Subtask{{Done: true}, {Done: true}},
			status:      "Plan",
			wantPercent: 100, wantDone: 2, wantTotal: 2,
		},
		{
			name:        "no checklist falls back to column position",
			subs:        nil,
			status:      "Develop",
			wantPercent: 33, wantDone: 0, wantTotal: 0,
		},
		{
			name:        "no checklist in done column is 100",
			subs:        nil,
			status:      "Done",
			wantPercent: 100, wantDone: 0, wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pct, done, total := childTaskProgress(tt.subs, tt.status, cols)
			if pct != tt.wantPercent || done != tt.wantDone || total != tt.wantTotal {
				t.Errorf("childTaskProgress() = (%d,%d,%d), want (%d,%d,%d)",
					pct, done, total, tt.wantPercent, tt.wantDone, tt.wantTotal)
			}
		})
	}
}

func TestProgressSummary(t *testing.T) {
	tests := []struct {
		name string
		p    *TaskProgress
		want string
	}{
		{
			name: "feature with children",
			p: &TaskProgress{Kind: db.KindFeature, Title: "Epic", Percent: 67, DoneCount: 2,
				TotalCount: 3, Board: ProgressBoard{DoneColumn: "Done"}},
			want: `Feature "Epic" is 67% complete — 2 of 3 child tasks done (in "Done").`,
		},
		{
			name: "feature without children",
			p:    &TaskProgress{Kind: db.KindFeature, Title: "Empty", Percent: 0, Status: "Inbox"},
			want: `Feature "Empty" has no child tasks yet; currently in column "Inbox" (0%).`,
		},
		{
			name: "task with checklist",
			p:    &TaskProgress{Kind: db.KindTask, Title: "Impl", Percent: 50, DoneCount: 2, TotalCount: 4},
			want: `Task "Impl" is 50% complete — 2 of 4 checklist items done.`,
		},
		{
			name: "task without checklist",
			p:    &TaskProgress{Kind: db.KindTask, Title: "Plain", Percent: 100, Status: "Done"},
			want: `Task "Plain" is in column "Done" (100%).`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := progressSummary(tt.p); got != tt.want {
				t.Errorf("progressSummary() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}
