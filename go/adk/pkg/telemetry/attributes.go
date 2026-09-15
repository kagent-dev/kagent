package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/v2/model"
)

const (
	captureMessageContentEnvVar = "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"
	maxSpanPayloadBytes         = 32 * 1024
)

// OTel GenAI semantic-convention attributes for execute_tool spans. ADK only
// records the tool exchange under its own gcp.vertex.agent.* keys, so backends
// that follow the GenAI conventions render tool spans with an empty body
// (kagent-dev/kagent#2734). The semconv package pinned by this module does not
// export these keys yet.
var (
	genAIToolCallArgumentsKey = attribute.Key("gen_ai.tool.call.arguments")
	genAIToolCallResultKey    = attribute.Key("gen_ai.tool.call.result")
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

// SetToolCallAttributes stamps the GenAI tool-call payload attributes onto the
// current execute_tool span. It is meant to run from an after-tool callback,
// which ADK invokes inside the execute_tool span context. Payload capture
// follows the same OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT switch as
// the model request/response attributes. When the tool returned an error the
// span status is set to Error so failed calls are distinguishable in the trace.
func SetToolCallAttributes(ctx context.Context, args, result map[string]any, toolErr error) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(
		genAIToolCallArgumentsKey.String(marshalToolPayload(args)),
		genAIToolCallResultKey.String(marshalToolPayload(result)),
	)
	if toolErr != nil {
		span.SetStatus(codes.Error, toolErr.Error())
	}
}

// marshalToolPayload serializes a tool argument or result map, normalizing a
// nil map to an empty JSON object so the attribute is always well-formed JSON.
func marshalToolPayload(payload map[string]any) string {
	if payload == nil {
		return "{}"
	}
	return marshalSpanPayload(payload)
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

// SetMessageMetadataAttributes sets scalar values from an A2A message's metadata as span attributes.
func SetMessageMetadataAttributes(ctx context.Context, metadata map[string]any) {
	if len(metadata) == 0 {
		return
	}
	var attrs []attribute.KeyValue
	for k, v := range metadata {
		key := "a2a.message.metadata." + k
		switch val := v.(type) {
		case string:
			if val != "" {
				attrs = append(attrs, attribute.String(key, val))
			}
		case bool:
			attrs = append(attrs, attribute.Bool(key, val))
		case float64:
			attrs = append(attrs, attribute.String(key, fmt.Sprintf("%g", val)))
		case int:
			attrs = append(attrs, attribute.Int(key, val))
		case int64:
			attrs = append(attrs, attribute.Int64(key, val))
		}
	}
	setSpanAttributes(ctx, attrs...)
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
