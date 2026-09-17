package tracing

import (
	"slices"
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
		{name: "claude", telemetry: RuntimeTelemetry{HarnessKind: HarnessKindClaude, AgentName: "a-h", AgentNamespace: "kagent"}},
		{name: "codex", telemetry: RuntimeTelemetry{HarnessKind: HarnessKindCodex, AgentName: "a-h", AgentNamespace: "kagent"}},
		{name: "capture without identity", telemetry: RuntimeTelemetry{CaptureContent: true, MaxCaptureBytes: 1024}},
		{name: "unknown kind", telemetry: RuntimeTelemetry{HarnessKind: "gemini", AgentName: "a-h", AgentNamespace: "kagent"}, wantError: true},
		{name: "identity without kind", telemetry: RuntimeTelemetry{AgentName: "a-h", AgentNamespace: "kagent"}, wantError: true},
		{name: "identity without name", telemetry: RuntimeTelemetry{HarnessKind: HarnessKindCodex, AgentNamespace: "kagent"}, wantError: true},
		{name: "identity without namespace", telemetry: RuntimeTelemetry{HarnessKind: HarnessKindCodex, AgentName: "a-h"}, wantError: true},
		{name: "padded agent name", telemetry: RuntimeTelemetry{HarnessKind: HarnessKindCodex, AgentName: " a-h ", AgentNamespace: "kagent"}, wantError: true},
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
	got := RuntimeTelemetry{HarnessKind: HarnessKindCodex, AgentName: "reporter-codex", AgentNamespace: "team"}.Identity()
	want := []attribute.KeyValue{
		attribute.String(AttributeHarnessKind, "codex"),
		attribute.String(AttributeAgentName, "reporter-codex"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Identity() = %v, want %v", got, want)
	}
}

func TestMergeResourceAttributes(t *testing.T) {
	owned := RuntimeTelemetry{HarnessKind: HarnessKindClaude, AgentName: "reporter-claude"}.Identity()
	for _, test := range []struct {
		name     string
		existing string
		owned    []attribute.KeyValue
		want     string
	}{
		{name: "empty existing", owned: owned, want: "gen_ai.agent.name=reporter-claude,kagent.harness.kind=claude"},
		{
			name: "user attributes preserved", existing: "deployment.environment=prod,team=sre", owned: owned,
			want: "deployment.environment=prod,team=sre,gen_ai.agent.name=reporter-claude,kagent.harness.kind=claude",
		},
		{
			name: "owned key replaces user value", existing: "kagent.harness.kind=codex,team=sre", owned: owned,
			want: "team=sre,gen_ai.agent.name=reporter-claude,kagent.harness.kind=claude",
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
		{name: "empty owned value dropped", owned: []attribute.KeyValue{attribute.String("kagent.harness.kind", "  ")}, want: ""},
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
	environment := []string{"PATH=/usr/bin", "OTEL_RESOURCE_ATTRIBUTES=team=sre,kagent.harness.kind=stale", "HOME=/data"}
	got := ResourceEnvironment(environment, RuntimeTelemetry{HarnessKind: HarnessKindCodex}.Identity())
	want := []string{"PATH=/usr/bin", "HOME=/data", "OTEL_RESOURCE_ATTRIBUTES=team=sre,kagent.harness.kind=codex"}
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
