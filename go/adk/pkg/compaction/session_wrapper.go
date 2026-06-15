package compaction

import (
	"context"
	"iter"

	adksession "google.golang.org/adk/session"
)

// NewCompactingService wraps real so that Get returns a compacted view of
// the session: compaction marker events are removed and replaced by synthetic
// summary events. All other Service methods delegate unchanged.
//
// AppendEvent unwraps *compactedSession before delegating so that storage
// implementations that type-assert the session (e.g. in-memory service) still
// receive the concrete type they expect.
func NewCompactingService(real adksession.Service, agentName string) adksession.Service {
	return &compactingService{real: real, agentName: agentName}
}

type compactingService struct {
	real      adksession.Service
	agentName string
}

func (s *compactingService) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	return s.real.Create(ctx, req)
}

func (s *compactingService) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	resp, err := s.real.Get(ctx, req)
	if err != nil || resp == nil || resp.Session == nil {
		return resp, err
	}
	events := collectEvents(resp.Session)
	compacted := BuildCompactedEventList(s.agentName, events)
	return &adksession.GetResponse{
		Session: &compactedSession{
			Session:   resp.Session,
			compacted: eventSlice(compacted),
		},
	}, nil
}

func (s *compactingService) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	return s.real.List(ctx, req)
}

func (s *compactingService) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	return s.real.Delete(ctx, req)
}

// AppendEvent unwraps *compactedSession to its underlying Session before
// delegating. The in-memory service does a concrete type assertion on the
// Session parameter; passing the wrapper would cause that assertion to fail.
func (s *compactingService) AppendEvent(ctx context.Context, sess adksession.Session, event *adksession.Event) error {
	if cs, ok := sess.(*compactedSession); ok {
		return s.real.AppendEvent(ctx, cs.Session, event)
	}
	return s.real.AppendEvent(ctx, sess, event)
}

// compactedSession wraps a real Session and overrides Events() to return the
// compacted event list. All other Session methods delegate to the real session.
type compactedSession struct {
	adksession.Session
	compacted eventSlice
}

func (s *compactedSession) Events() adksession.Events {
	return s.compacted
}

// eventSlice implements adksession.Events over a plain slice.
type eventSlice []*adksession.Event

func (e eventSlice) All() iter.Seq[*adksession.Event] {
	return func(yield func(*adksession.Event) bool) {
		for _, ev := range e {
			if !yield(ev) {
				return
			}
		}
	}
}

func (e eventSlice) Len() int { return len(e) }

func (e eventSlice) At(i int) *adksession.Event { return e[i] }
