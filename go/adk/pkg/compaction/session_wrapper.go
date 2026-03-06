package compaction

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
)

// CompactingSessionService wraps a session service and automatically performs
// compaction when events are appended to sessions.
//
// This is a temporary solution until upstream adk-go releases compaction support
// in runner.Config (https://github.com/google/adk-go/pull/300). Once that's released,
// this wrapper can be removed in favor of native compaction.
type CompactingSessionService struct {
	wrapped    adksession.Service
	config     Config
	model      adkmodel.LLM
	compactors sync.Map // sessionID -> *Compactor
}

// NewCompactingSessionService wraps an existing session service with compaction support.
func NewCompactingSessionService(wrapped adksession.Service, config Config, model adkmodel.LLM) (*CompactingSessionService, error) {
	if model == nil && config.Enabled {
		return nil, fmt.Errorf("model is required when compaction is enabled")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &CompactingSessionService{
		wrapped: wrapped,
		config:  config,
		model:   model,
	}, nil
}

// Create delegates to the wrapped service.
func (s *CompactingSessionService) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	return s.wrapped.Create(ctx, req)
}

// Get delegates to the wrapped service.
func (s *CompactingSessionService) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	return s.wrapped.Get(ctx, req)
}

// List delegates to the wrapped service.
func (s *CompactingSessionService) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	return s.wrapped.List(ctx, req)
}

// Delete delegates to the wrapped service and cleans up compactor.
func (s *CompactingSessionService) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	err := s.wrapped.Delete(ctx, req)
	if err == nil {
		s.compactors.Delete(req.SessionID)
	}
	return err
}

// AppendEvent appends the event and potentially triggers compaction.
func (s *CompactingSessionService) AppendEvent(ctx context.Context, session adksession.Session, event *adksession.Event) error {
	// First append the event
	if err := s.wrapped.AppendEvent(ctx, session, event); err != nil {
		return err
	}

	// If compaction is disabled or this is a system event, skip compaction
	if !s.config.Enabled || event.Author == "system" {
		return nil
	}

	// Get or create compactor for this session
	compactor := s.getOrCreateCompactor(session.ID())

	// Attempt compaction (non-blocking, runs in background)
	// Detach from request context to prevent premature cancellation
	go func() {
		// Create a detached context with a reasonable timeout for compaction work
		compactionCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		log := logr.FromContextOrDiscard(compactionCtx)
		compactionEvent, err := compactor.MaybeCompact(compactionCtx, session, event.InvocationID)
		if err != nil {
			log.Error(err, "Compaction failed", "sessionID", session.ID())
			return
		}

		if compactionEvent != nil {
			// Append compaction event to session
			if err := s.wrapped.AppendEvent(compactionCtx, session, compactionEvent); err != nil {
				log.Error(err, "Failed to append compaction event", "sessionID", session.ID())
			}
		}
	}()

	return nil
}

// getOrCreateCompactor returns the compactor for a session, creating it if needed.
func (s *CompactingSessionService) getOrCreateCompactor(sessionID string) *Compactor {
	if val, ok := s.compactors.Load(sessionID); ok {
		return val.(*Compactor)
	}

	// Create new compactor
	compactor, err := New(s.config, s.model)
	if err != nil {
		// Return a disabled compactor as fallback
		disabledConfig := s.config
		disabledConfig.Enabled = false
		compactor, _ = New(disabledConfig, s.model)
	}

	// Store and return
	actual, _ := s.compactors.LoadOrStore(sessionID, compactor)
	return actual.(*Compactor)
}
