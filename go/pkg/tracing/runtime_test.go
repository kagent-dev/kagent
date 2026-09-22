package tracing

import (
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestRuntimeTelemetryValidate(t *testing.T) {
	for _, test := range []struct {
		name      string
		telemetry RuntimeTelemetry
		wantError bool
	}{
		{name: "empty is valid"},
		{name: "claude", telemetry: RuntimeTelemetry{Runtime: RuntimeClaude, AgentName: "a-h", AgentNamespace: "kagent"}},
		{name: "codex", telemetry: RuntimeTelemetry{Runtime: RuntimeCodex, AgentName: "a-h", AgentNamespace: "kagent"}},
		{name: "adk go", telemetry: RuntimeTelemetry{Runtime: RuntimeADKGo, AgentName: "a-h", AgentNamespace: "kagent"}},
		{name: "model identity", telemetry: RuntimeTelemetry{Runtime: RuntimeCodex, AgentName: "a-h", AgentNamespace: "kagent", Provider: "openai", Model: "gpt-5.2-codex"}},
		{name: "capture without identity", telemetry: RuntimeTelemetry{CaptureContent: true, MaxCaptureBytes: 1024}},
		{name: "unknown runtime", telemetry: RuntimeTelemetry{Runtime: "gemini", AgentName: "a-h", AgentNamespace: "kagent"}, wantError: true},
		{name: "identity without runtime", telemetry: RuntimeTelemetry{AgentName: "a-h", AgentNamespace: "kagent"}, wantError: true},
		{name: "identity without name", telemetry: RuntimeTelemetry{Runtime: RuntimeCodex, AgentNamespace: "kagent"}, wantError: true},
		{name: "identity without namespace", telemetry: RuntimeTelemetry{Runtime: RuntimeCodex, AgentName: "a-h"}, wantError: true},
		{name: "padded agent name", telemetry: RuntimeTelemetry{Runtime: RuntimeCodex, AgentName: " a-h ", AgentNamespace: "kagent"}, wantError: true},
		{name: "padded model", telemetry: RuntimeTelemetry{Runtime: RuntimeCodex, AgentName: "a-h", AgentNamespace: "kagent", Model: "gpt "}, wantError: true},
		{name: "negative limit", telemetry: RuntimeTelemetry{MaxCaptureBytes: -1}, wantError: true},
		{name: "limit above ceiling", telemetry: RuntimeTelemetry{MaxCaptureBytes: MaxCaptureBytes + 1}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.telemetry.Validate()
			if (err != nil) != test.wantError {
				t.Fatalf("Validate() error = %v, wantError = %v", err, test.wantError)
			}
		})
	}
}

func TestCaptureLimitDefaultsOff(t *testing.T) {
	for _, test := range []struct {
		name      string
		telemetry RuntimeTelemetry
		want      int
	}{
		{name: "disabled by default", telemetry: RuntimeTelemetry{}, want: 0},
		{name: "disabled ignores limit", telemetry: RuntimeTelemetry{MaxCaptureBytes: 1024}, want: 0},
		{name: "enabled without limit", telemetry: RuntimeTelemetry{CaptureContent: true}, want: DefaultCaptureBytes},
		{name: "enabled with limit", telemetry: RuntimeTelemetry{CaptureContent: true, MaxCaptureBytes: 1024}, want: 1024},
		{name: "clamped to ceiling", telemetry: RuntimeTelemetry{CaptureContent: true, MaxCaptureBytes: MaxCaptureBytes * 4}, want: MaxCaptureBytes},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.telemetry.CaptureLimit(); got != test.want {
				t.Fatalf("CaptureLimit() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestIdentityOmitsUnsetFields(t *testing.T) {
	if got := (RuntimeTelemetry{}).Identity(); len(got) != 0 {
		t.Fatalf("Identity() = %v, want none", got)
	}
	got := RuntimeTelemetry{Runtime: RuntimeCodex, AgentName: "reporter-codex", AgentNamespace: "team"}.Identity()
	want := []attribute.KeyValue{
		attribute.String(AttributeRuntime, "codex"),
		attribute.String(AttributeAgentName, "reporter-codex"),
		attribute.String(AttributeAgentID, "team/reporter-codex"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Identity() = %v, want %v", got, want)
	}
	// A namespace alone cannot qualify an agent, so no agent id is reported.
	if got := (RuntimeTelemetry{AgentNamespace: "team"}).Identity(); len(got) != 0 {
		t.Fatalf("Identity() = %v, want none", got)
	}
	withModel := RuntimeTelemetry{
		Runtime: RuntimeClaude, AgentName: "reporter-claude", AgentNamespace: "team", Provider: "anthropic", Model: "claude-sonnet-4-5",
	}.Identity()
	wantModel := []attribute.KeyValue{
		attribute.String(AttributeRuntime, "claude"),
		attribute.String(AttributeAgentName, "reporter-claude"),
		attribute.String(AttributeAgentID, "team/reporter-claude"),
		attribute.String(AttributeProviderName, "anthropic"),
		attribute.String(AttributeRequestModel, "claude-sonnet-4-5"),
	}
	if !slices.Equal(withModel, wantModel) {
		t.Fatalf("Identity() = %v, want %v", withModel, wantModel)
	}
}

func TestChildResourceAddsTheCompiledNamespace(t *testing.T) {
	telemetry := RuntimeTelemetry{Runtime: RuntimeCodex, AgentName: "reporter-codex", AgentNamespace: "team"}
	want := append(telemetry.Identity(), attribute.String("service.namespace", "team"))
	if got := telemetry.ChildResource(); !slices.Equal(got, want) {
		t.Fatalf("ChildResource() = %v, want %v", got, want)
	}
	if got := (RuntimeTelemetry{}).ChildResource(); len(got) != 0 {
		t.Fatalf("ChildResource() = %v, want none without an identity", got)
	}
}

func TestRequestIdentity(t *testing.T) {
	got := RequestIdentity("ctx-1", "task-1", false)
	want := []attribute.KeyValue{
		attribute.String(AttributeConversationID, "ctx-1"),
		attribute.String(AttributeTaskID, "task-1"),
		attribute.String(AttributeSegment, SegmentInitial),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("RequestIdentity() = %v, want %v", got, want)
	}
	// A request without identity still says which segment it is.
	if got := RequestIdentity("", "", true); !slices.Equal(got, []attribute.KeyValue{attribute.String(AttributeSegment, SegmentResumed)}) {
		t.Fatalf("RequestIdentity() = %v, want only the segment", got)
	}
}

func TestMergeResourceAttributes(t *testing.T) {
	owned := RuntimeTelemetry{Runtime: RuntimeClaude, AgentName: "reporter-claude"}.Identity()
	for _, test := range []struct {
		name     string
		existing string
		owned    []attribute.KeyValue
		want     string
	}{
		{name: "empty existing", owned: owned, want: "gen_ai.agent.name=reporter-claude,kagent.runtime=claude"},
		{
			name: "user attributes preserved", existing: "deployment.environment=prod,team=sre", owned: owned,
			want: "deployment.environment=prod,team=sre,gen_ai.agent.name=reporter-claude,kagent.runtime=claude",
		},
		{
			name: "owned key replaces user value", existing: "kagent.runtime=codex,team=sre", owned: owned,
			want: "team=sre,gen_ai.agent.name=reporter-claude,kagent.runtime=claude",
		},
		{
			// The OpenTelemetry SDK resolves the same string to the last value, so
			// the wrapper and its native child cannot disagree about this key.
			name: "duplicate user key keeps the last", existing: "team=sre,team=platform", owned: nil,
			want: "team=platform",
		},
		{
			name: "duplicate user key keeps its original position", existing: "team=sre,zone=a,team=platform", owned: nil,
			want: "team=platform,zone=a",
		},
		{name: "unparsable entries dropped", existing: "novalue, ,=orphan,team=sre", owned: nil, want: "team=sre"},
		{name: "empty owned value dropped", owned: []attribute.KeyValue{attribute.String("kagent.runtime", "  ")}, want: ""},
		{
			name: "value is percent encoded", owned: []attribute.KeyValue{attribute.String("gen_ai.agent.name", "a b,c=d")},
			want: "gen_ai.agent.name=a%20b%2Cc%3Dd",
		},
		{name: "nothing to merge", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := MergeResourceAttributes(test.existing, test.owned); got != test.want {
				t.Fatalf("MergeResourceAttributes() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResourceEnvironmentReplacesOnlyTheResourceVariable(t *testing.T) {
	environment := []string{"PATH=/usr/bin", "OTEL_RESOURCE_ATTRIBUTES=team=sre,kagent.runtime=stale", "HOME=/data"}
	got := ResourceEnvironment(environment, RuntimeTelemetry{Runtime: RuntimeCodex}.Identity())
	want := []string{"PATH=/usr/bin", "HOME=/data", "OTEL_RESOURCE_ATTRIBUTES=team=sre,kagent.runtime=codex"}
	if !slices.Equal(got, want) {
		t.Fatalf("ResourceEnvironment() = %v, want %v", got, want)
	}
}

func TestResourceEnvironmentDropsTheVariableWhenNothingRemains(t *testing.T) {
	got := ResourceEnvironment([]string{"OTEL_RESOURCE_ATTRIBUTES=", "PATH=/usr/bin"}, nil)
	if !slices.Equal(got, []string{"PATH=/usr/bin"}) {
		t.Fatalf("ResourceEnvironment() = %v, want only PATH", got)
	}
}

// TestAttributeNamesAreNotNamespaces keeps every published attribute name from
// being a dotted prefix of another. A backend that stores attributes as a
// nested document cannot hold a value and an object at the same path, so a name
// that is also a namespace silently costs a consumer the value: ClickHouse's
// JSON type renders both and a reader keeps the last, while Elasticsearch
// rejects the mapping. The wire format is flat, so nothing upstream of storage
// reports the conflict.
func TestAttributeNamesAreNotNamespaces(t *testing.T) {
	names := []string{
		AttributeOperationName, AttributeRuntime, AttributeAgentName, AttributeAgentID, AttributeProviderName,
		AttributeRequestModel, AttributeConversationID, AttributeTaskID, AttributeUserID, AttributeMethod,
		AttributeTaskState, AttributeSegment, AttributeDisposition, AttributeInputMessages, AttributeInputTruncated,
		AttributeOutputMessages, AttributeOutputTruncated, AttributeErrorType, AttributeLinkRelationship,
	}
	for _, namespace := range names {
		for _, name := range names {
			if name != namespace && strings.HasPrefix(name, namespace+".") {
				t.Errorf("%q is both an attribute and the namespace of %q", namespace, name)
			}
		}
	}
}
