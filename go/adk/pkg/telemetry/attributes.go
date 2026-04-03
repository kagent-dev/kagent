package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/model"
)

const (
	captureMessageContentEnvVar = "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"
	maxSpanPayloadBytes         = 32 * 1024
)

type kagentSpanAttributesKey struct{}

type kagentAttributesSpanProcessor struct{}

func (kagentAttributesSpanProcessor) OnStart(parent context.Context, span sdktrace.ReadWriteSpan) {
	if attrs := contextAttributes(parent); len(attrs) > 0 {
		span.SetAttributes(stringAttributes(attrs)...)
	}
}

func (kagentAttributesSpanProcessor) OnEnd(sdktrace.ReadOnlySpan) {}

func (kagentAttributesSpanProcessor) Shutdown(context.Context) error { return nil }

func (kagentAttributesSpanProcessor) ForceFlush(context.Context) error { return nil }

// SetLLMRequestAttributes stamps the missing request payload attribute onto the
// current generate_content span.
func SetLLMRequestAttributes(ctx context.Context, _ string, req *model.LLMRequest) {
	setSpanAttributes(ctx, attribute.String("gcp.vertex.agent.llm_request", marshalSpanPayload(req)))
}

// SetLLMResponseAttributes stamps the missing response payload attribute onto the
// current generate_content span.
func SetLLMResponseAttributes(ctx context.Context, resp *model.LLMResponse) {
	setSpanAttributes(ctx, attribute.String("gcp.vertex.agent.llm_response", marshalSpanPayload(resp)))
}

func contextAttributes(ctx context.Context) map[string]string {
	attrs, _ := ctx.Value(kagentSpanAttributesKey{}).(map[string]string)
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

func mergeAttributes(existing, updates map[string]string) map[string]string {
	if len(existing) == 0 && len(updates) == 0 {
		return nil
	}
	merged := make(map[string]string, len(existing)+len(updates))
	for key, value := range existing {
		if value != "" {
			merged[key] = value
		}
	}
	for key, value := range updates {
		if value != "" {
			merged[key] = value
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func stringAttributes(attrs map[string]string) []attribute.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, 0, len(attrs))
	for key, value := range attrs {
		if value != "" {
			out = append(out, attribute.String(key, value))
		}
	}
	return out
}

func setSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() || len(attrs) == 0 {
		return
	}
	span.SetAttributes(attrs...)
}

func marshalSpanPayload(value any) string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(captureMessageContentEnvVar)), "false") {
		return "{}"
	}
	if value == nil {
		return "{}"
	}
	payload, err := json.Marshal(value)
	if err != nil {
		fallback, marshalErr := json.Marshal(map[string]string{"marshal_error": err.Error()})
		if marshalErr != nil {
			return "{}"
		}
		return string(fallback)
	}
	if len(payload) <= maxSpanPayloadBytes {
		return string(payload)
	}
	truncated, err := json.Marshal(map[string]any{
		"truncated":      true,
		"original_size":  len(payload),
		"payload_prefix": string(payload[:maxSpanPayloadBytes]),
	})
	if err != nil {
		return "{}"
	}
	return string(truncated)
}
