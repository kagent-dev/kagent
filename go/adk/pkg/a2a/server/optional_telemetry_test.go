package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"go.opentelemetry.io/otel"

	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry"
)

func TestTelemetryInitializationFailureKeepsA2AAvailable(t *testing.T) {
	for _, test := range []struct {
		name     string
		exporter string
		disabled string
		wantErr  bool
	}{
		{name: "unconfigured", exporter: "none"},
		{name: "disabled", exporter: "otlp", disabled: "true"},
		{name: "invalid exporter", exporter: "invalid-exporter", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACES_EXPORTER", test.exporter)
			t.Setenv("OTEL_METRICS_EXPORTER", "none")
			t.Setenv("OTEL_LOGS_EXPORTER", "none")
			t.Setenv("OTEL_SDK_DISABLED", test.disabled)
			tracer, meter, handler := otel.GetTracerProvider(), otel.GetMeterProvider(), otel.GetErrorHandler()
			propagator := otel.GetTextMapPropagator()
			t.Cleanup(func() {
				otel.SetTracerProvider(tracer)
				otel.SetMeterProvider(meter)
				otel.SetTextMapPropagator(propagator)
				otel.SetErrorHandler(handler)
			})
			logger := slog.New(slog.DiscardHandler)
			providers, err := telemetry.Init(logging.IntoContext(t.Context(), logger), telemetry.Options{})
			if (err != nil) != test.wantErr {
				t.Fatalf("Init error = %v, wantErr %v", err, test.wantErr)
			}
			otel.Handle(err)
			srv, err := NewA2AServer(a2atype.AgentCard{}, substrateExecutor{}, logger,
				ServerConfig{Port: "0", HealthPaths: []string{"/readyz"}, Flush: providers.ForceFlush})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(srv.httpServer.Handler)
			t.Cleanup(func() { server.Close(); srv.grpcServer.Stop() })
			response, err := server.Client().Get(server.URL + "/readyz")
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("readiness = %d", response.StatusCode)
			}
			params, err := json.Marshal(&a2atype.SendMessageRequest{
				Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
			})
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL,
				strings.NewReader(`{"jsonrpc":"2.0","id":"1","method":"SendMessage","params":`+string(params)+`}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(a2atype.SvcParamVersion, string(a2atype.Version))
			response, err = server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "done") {
				t.Fatalf("A2A response = %d %s", response.StatusCode, body)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := providers.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
