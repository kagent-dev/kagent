package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kagent-dev/kagent/go/plugins/nats-activity-feed/internal/config"
	"github.com/kagent-dev/kagent/go/plugins/nats-activity-feed/internal/feed"
	"github.com/kagent-dev/kagent/go/plugins/nats-activity-feed/internal/sse"
	"github.com/kagent-dev/kagent/go/plugins/nats-activity-feed/internal/ui"
	"github.com/nats-io/nats.go"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Connect to NATS with auto-reconnect
	nc, err := nats.Connect(cfg.NATSAddr,
		nats.MaxReconnects(-1),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Printf("NATS disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			log.Println("NATS reconnected")
		}),
	)
	if err != nil {
		log.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()

	hub := sse.NewHub(cfg.BufferSize)

	sub, err := feed.NewSubscriber(nc, cfg.Subject, hub)
	if err != nil {
		log.Fatalf("subscriber: %v", err)
	}
	defer sub.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", hub.ServeSSE)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok")) //nolint:errcheck
	})
	mux.Handle("/", ui.Handler())

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	log.Printf("nats-activity-feed listening on %s (NATS: %s, subject: %s, buffer: %d)",
		cfg.Addr, cfg.NATSAddr, cfg.Subject, cfg.BufferSize)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http: %v", err)
	}
}
