package telemetry

import (
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

type traceReceiver struct {
	collectortrace.UnimplementedTraceServiceServer
	t        *testing.T
	requests chan *collectortrace.ExportTraceServiceRequest
}

func (r *traceReceiver) Export(ctx context.Context, request *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	require.Equal(r.t, []string{"override-token"}, md.Get("authorization"))
	r.requests <- request
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func TestExporterEnvironmentOverridesFileSettings(t *testing.T) {
	for _, protocol := range []string{"http/protobuf", "grpc"} {
		t.Run(protocol, func(t *testing.T) {
			restoreGlobals(t)
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=override")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "authorization=override-token")
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
			requests := make(chan *collectortrace.ExportTraceServiceRequest, 4)
			var endpoint string
			if protocol == "grpc" {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				server := grpc.NewServer()
				collectortrace.RegisterTraceServiceServer(server, &traceReceiver{t: t, requests: requests})
				go func() { _ = server.Serve(listener) }()
				t.Cleanup(server.Stop)
				endpoint = "http://" + listener.Addr().String()
			} else {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "/custom/traces", r.URL.Path)
					require.Equal(t, "override-token", r.Header.Get("Authorization"))
					var body io.Reader = r.Body
					if r.Header.Get("Content-Encoding") == "gzip" {
						reader, err := gzip.NewReader(r.Body)
						require.NoError(t, err)
						defer reader.Close()
						body = reader
					}
					data, err := io.ReadAll(body)
					require.NoError(t, err)
					request := &collectortrace.ExportTraceServiceRequest{}
					require.NoError(t, proto.Unmarshal(data, request))
					requests <- request
					w.Header().Set("Content-Type", "application/x-protobuf")
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(server.Close)
				endpoint = server.URL + "/custom/traces"
			}
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", endpoint)
			providers, err := Init(t.Context(), Options{Config: Config{
				Traces:             &OTLPConfig{Endpoint: "http://127.0.0.1:1", Protocol: "grpc"},
				ResourceAttributes: map[string]string{"service.name": "configured"},
			}})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, providers.Shutdown(context.Background())) })
			_, span := otel.Tracer("explicit-config").Start(t.Context(), "delivered")
			span.SetAttributes(attribute.String("source", "config"))
			span.End()
			require.NoError(t, providers.ForceFlush(t.Context()))
			var request *collectortrace.ExportTraceServiceRequest
			select {
			case request = <-requests:
			case <-time.After(time.Second):
				t.Fatal("no exported span")
			}
			require.Len(t, request.ResourceSpans, 1)
			resource := request.ResourceSpans[0]
			attributes := map[string]string{}
			for _, item := range resource.Resource.Attributes {
				attributes[item.Key] = item.Value.GetStringValue()
			}
			require.Equal(t, "override", attributes["service.name"])
			require.Equal(t, "delivered", resource.ScopeSpans[0].Spans[0].Name)
			require.Equal(t, "config", resource.ScopeSpans[0].Spans[0].Attributes[0].Value.GetStringValue())
		})
	}
}
