package client

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReadCloser implements io.ReadCloser for testing
type mockReadCloser struct {
	*strings.Reader
}

func (m *mockReadCloser) Close() error {
	return nil
}

func newMockReadCloser(data string) io.ReadCloser {
	return &mockReadCloser{
		Reader: strings.NewReader(data),
	}
}

func TestStreamSseResponse(t *testing.T) {
	t.Run("should parse single SSE event with event and data", func(t *testing.T) {
		sseData := "event:message\ndata:hello world\n"
		reader := newMockReadCloser(sseData)

		ch := streamSseResponse(reader)

		// Read the event from the channel
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "message", event.Event)
			assert.Equal(t, []byte("hello world"), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}

		// Verify channel is closed
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for channel to close")
		}
	})

	t.Run("should parse multiple SSE events", func(t *testing.T) {
		sseData := "event:message\ndata:first message\nevent:update\ndata:second message\n"
		reader := newMockReadCloser(sseData)

		ch := streamSseResponse(reader)

		// Read first event
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "message", event.Event)
			assert.Equal(t, []byte("first message"), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for first event")
		}

		// Read second event
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "update", event.Event)
			assert.Equal(t, []byte("second message"), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for second event")
		}

		// Verify channel is closed
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for channel to close")
		}
	})

	t.Run("should handle data-only events without event type", func(t *testing.T) {
		sseData := "data:message without event type\n"
		reader := newMockReadCloser(sseData)

		ch := streamSseResponse(reader)

		// Read the event from the channel
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "", event.Event) // Empty event type
			assert.Equal(t, []byte("message without event type"), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}

		// Verify channel is closed
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for channel to close")
		}
	})

	t.Run("should handle empty data", func(t *testing.T) {
		sseData := "event:empty\ndata:\n"
		reader := newMockReadCloser(sseData)

		ch := streamSseResponse(reader)

		// Read the event from the channel
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "empty", event.Event)
			assert.Equal(t, ([]byte)(nil), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}

		// Verify channel is closed
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for channel to close")
		}
	})

	t.Run("should handle JSON data", func(t *testing.T) {
		jsonData := `{"message": "hello", "count": 42}`
		sseData := "event:json\ndata:" + jsonData + "\n"
		reader := newMockReadCloser(sseData)

		ch := streamSseResponse(reader)

		// Read the event from the channel
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "json", event.Event)
			assert.Equal(t, []byte(jsonData), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}

		// Verify channel is closed
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for channel to close")
		}
	})

	t.Run("should handle empty input", func(t *testing.T) {
		reader := newMockReadCloser("")

		ch := streamSseResponse(reader)

		// Verify channel is closed immediately
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for channel to close")
		}
	})

	t.Run("should ignore lines that don't start with event: or data:", func(t *testing.T) {
		sseData := "comment: this is a comment\nevent:test\nother line\ndata:test data\n"
		reader := newMockReadCloser(sseData)

		ch := streamSseResponse(reader)

		// Read the event from the channel
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "test", event.Event)
			assert.Equal(t, []byte("test data"), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}

		// Verify channel is closed
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for channel to close")
		}
	})

	t.Run("should handle event type without corresponding data", func(t *testing.T) {
		sseData := "event:orphan\nevent:complete\ndata:complete data\n"
		reader := newMockReadCloser(sseData)

		ch := streamSseResponse(reader)

		// Should only receive the complete event (the orphan event has no data line)
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "complete", event.Event)
			assert.Equal(t, []byte("complete data"), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}

		// Verify channel is closed
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for channel to close")
		}
	})

	t.Run("should handle complex multiline scenario", func(t *testing.T) {
		sseData := `event:start
data:starting process

event:progress
data:50% complete

event:end
data:process finished
`
		reader := newMockReadCloser(sseData)

		ch := streamSseResponse(reader)

		// Read first event
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "start", event.Event)
			assert.Equal(t, []byte("starting process"), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for first event")
		}

		// Read second event
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "progress", event.Event)
			assert.Equal(t, []byte("50% complete"), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for second event")
		}

		// Read third event
		select {
		case event := <-ch:
			require.NotNil(t, event)
			assert.Equal(t, "end", event.Event)
			assert.Equal(t, []byte("process finished"), event.Data)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for third event")
		}

		// Verify channel is closed
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for channel to close")
		}
	})
}
