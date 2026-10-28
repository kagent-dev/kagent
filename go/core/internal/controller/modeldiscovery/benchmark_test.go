package modeldiscovery

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	api "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func benchmarkPayload(models, padding int) []byte {
	var body strings.Builder
	body.WriteString(`{"object":"list","data":[`)
	for i := range models {
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"id":"model-%d","object":"model","created":1781308800,"owned_by":"gcp.vertex_ai","name":"Example%s","context_window":1000000,"context_length":1000000,"max_input_tokens":1000000,"max_tokens":64000,"capabilities":{"tools":true,"reasoning":true,"structured_outputs":true,"temperature":false},"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"supported_parameters":["tools","temperature","max_tokens","response_format"]}`, i, strings.Repeat("x", padding))
	}
	body.WriteString(`]}`)
	return []byte(body.String())
}

// Independent decodes avoid artificially sharing strings between sources.
// This measures catalog storage and reads, excluding informers and networking.
func benchmarkController(b *testing.B, payload []byte) *Controller {
	b.Helper()
	c := &Controller{entries: make(map[string]*entry)}
	for i := range 100 {
		models, err := parseModels(payload)
		if err != nil {
			b.Fatal(err)
		}
		input := source{Ref: types.NamespacedName{Namespace: "team", Name: fmt.Sprintf("source-%d", i)}, UID: types.UID(fmt.Sprintf("uid-%d", i)), Generation: 1, Type: api.ModelProviderOpenAI, Endpoint: "https://gateway.example/v1"}
		snapshot := pending(input)
		snapshot.State = apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT
		snapshot.Models = models
		c.entries[input.ResourceName()] = &entry{input: input, snapshot: snapshot}
	}
	return c
}

func BenchmarkCatalogScale(b *testing.B) {
	payload := benchmarkPayload(1000, 0)
	b.Run("Retained100x1000", func(b *testing.B) {
		b.ReportAllocs()
		var retained uint64
		for b.Loop() {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			c := benchmarkController(b, payload)
			runtime.GC()
			runtime.ReadMemStats(&after)
			retained += after.HeapAlloc - before.HeapAlloc
			runtime.KeepAlive(c)
		}
		b.ReportMetric(float64(retained)/float64(b.N), "retained-B")
		b.ReportMetric(float64(len(payload)), "source-JSON-B")
	})
	b.Run("Get1000", func(b *testing.B) {
		c := benchmarkController(b, payload)
		ref := types.NamespacedName{Namespace: "team", Name: "source-0"}
		b.ReportAllocs()
		for b.Loop() {
			runtime.KeepAlive(c.Get(ref))
		}
	})
	b.Run("List100x1000", func(b *testing.B) {
		c := benchmarkController(b, payload)
		b.ReportAllocs()
		for b.Loop() {
			runtime.KeepAlive(c.List())
		}
	})
	b.Run("Replace100x1000FourWorkers", func(b *testing.B) {
		c := benchmarkController(b, payload)
		entries := make([]*entry, 0, len(c.entries))
		for _, e := range c.entries {
			entries = append(entries, e)
		}
		// Keep the old catalog resident while parsing replacements, with the same
		// concurrency cap as production. This excludes HTTP and scheduler overhead.
		b.ReportAllocs()
		for b.Loop() {
			var workers sync.WaitGroup
			for worker := range maxConcurrent {
				workers.Go(func() {
					for i := worker; i < len(entries); i += maxConcurrent {
						models, err := parseModels(payload)
						if err != nil {
							b.Error(err)
							return
						}
						c.mu.Lock()
						entries[i].snapshot.Models = models
						c.mu.Unlock()
					}
				})
			}
			workers.Wait()
		}
		runtime.KeepAlive(c)
	})
}

func BenchmarkCatalogNearBodyLimit(b *testing.B) {
	base := benchmarkPayload(1000, 0)
	payload := benchmarkPayload(1000, (maxBody-len(base)-1024)/1000)
	if _, err := parseModels(payload); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		models, err := parseModels(payload)
		if err != nil {
			b.Fatal(err)
		}
		runtime.KeepAlive(models)
	}
	b.ReportMetric(float64(len(payload)), "source-JSON-B")
}
