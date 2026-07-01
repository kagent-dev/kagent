package db

import "testing"

func TestValidStatus(t *testing.T) {
	tests := []struct {
		name   string
		status TaskStatus
		want   bool
	}{
		{name: "inbox", status: StatusInbox, want: true},
		{name: "plan", status: StatusPlan, want: true},
		{name: "develop", status: StatusDevelop, want: true},
		{name: "testing", status: StatusTesting, want: true},
		{name: "code review", status: StatusCodeReview, want: true},
		{name: "release", status: StatusRelease, want: true},
		{name: "done", status: StatusDone, want: true},
		{name: "empty", status: TaskStatus(""), want: false},
		{name: "invalid", status: TaskStatus("invalid"), want: false},
		{name: "wrong case", status: TaskStatus("inbox"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidStatus(tt.status); got != tt.want {
				t.Errorf("ValidStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestStatusWorkflow_Order(t *testing.T) {
	want := []TaskStatus{
		StatusInbox, StatusPlan, StatusDevelop, StatusTesting,
		StatusCodeReview, StatusRelease, StatusDone,
	}
	if len(StatusWorkflow) != len(want) {
		t.Fatalf("StatusWorkflow length = %d, want %d", len(StatusWorkflow), len(want))
	}
	for i := range want {
		if StatusWorkflow[i] != want[i] {
			t.Errorf("StatusWorkflow[%d] = %q, want %q", i, StatusWorkflow[i], want[i])
		}
	}
}

func TestValidUserAttachmentType(t *testing.T) {
	tests := []struct {
		name string
		typ  AttachmentType
		want bool
	}{
		{name: "file", typ: AttachmentTypeFile, want: true},
		{name: "link", typ: AttachmentTypeLink, want: true},
		{name: "attribute not user-addable", typ: AttachmentTypeAttribute, want: false},
		{name: "empty", typ: AttachmentType(""), want: false},
		{name: "invalid", typ: AttachmentType("image"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidUserAttachmentType(tt.typ); got != tt.want {
				t.Errorf("ValidUserAttachmentType(%q) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

func TestValidFileExtension(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{name: "markdown", filename: "DESIGN.md", want: true},
		{name: "uppercase pdf", filename: "report.PDF", want: true},
		{name: "xlsx", filename: "data.xlsx", want: true},
		{name: "no extension", filename: "README", want: false},
		{name: "disallowed", filename: "evil.exe", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidFileExtension(tt.filename); got != tt.want {
				t.Errorf("ValidFileExtension(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}
