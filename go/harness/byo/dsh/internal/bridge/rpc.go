package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const maxFrameBytes = 4 << 20

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type frame struct {
	message message
	err     error
}

type wire struct {
	writer   io.WriteCloser
	mu       sync.Mutex
	frames   chan frame
	done     chan struct{}
	readDone chan struct{}
}

func newWire(writer io.WriteCloser, reader io.Reader) *wire {
	w := &wire{writer: writer, frames: make(chan frame, 16), done: make(chan struct{}), readDone: make(chan struct{})}
	go w.read(reader)
	return w
}

func (w *wire) read(reader io.Reader) {
	defer close(w.frames)
	defer close(w.readDone)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxFrameBytes)
	for scanner.Scan() {
		var m message
		err := json.Unmarshal(scanner.Bytes(), &m)
		if err != nil || m.JSONRPC != "2.0" {
			w.deliver(frame{err: fmt.Errorf("invalid DSH ACP frame")})
			return
		}
		if !w.deliver(frame{message: m}) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		w.deliver(frame{err: fmt.Errorf("failed to read DSH ACP frame: %w", err)})
	}
}

func (w *wire) deliver(f frame) bool {
	select {
	case w.frames <- f:
		return true
	case <-w.done:
		return false
	}
}

func (w *wire) send(ctx context.Context, m message, params any) error {
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to encode ACP parameters: %w", err)
		}
		m.Params = data
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to encode ACP message: %w", err)
	}
	// Canceling a partially written frame invalidates the stream. Closing stdin
	// also interrupts a blocked pipe write; the process owner then reaps DSH.
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = w.writer.Close()
		close(interrupted)
	})
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := w.writer.Write(append(data, '\n')); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("failed to write ACP message: %w", err)
	}
	return ctx.Err()
}

func (w *wire) respond(ctx context.Context, id json.RawMessage, result any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to encode ACP response: %w", err)
	}
	return w.send(ctx, message{JSONRPC: "2.0", ID: id, Result: data}, nil)
}

func (w *wire) next(ctx context.Context) (message, error) {
	select {
	case <-ctx.Done():
		return message{}, ctx.Err()
	case f, ok := <-w.frames:
		if !ok {
			return message{}, fmt.Errorf("DSH ACP stream closed")
		}
		return f.message, f.err
	}
}

// Setup calls have no active turn, so requests are rejected and updates ignored.
func (w *wire) call(ctx context.Context, id string, method string, params any) (json.RawMessage, error) {
	if err := w.send(ctx, message{JSONRPC: "2.0", ID: json.RawMessage(id), Method: method}, params); err != nil {
		return nil, err
	}
	for {
		m, err := w.next(ctx)
		if err != nil {
			return nil, err
		}
		if m.Method != "" {
			if len(m.ID) != 0 {
				return nil, fmt.Errorf("unexpected ACP request during initialization")
			}
			continue
		}
		if string(m.ID) != id {
			return nil, fmt.Errorf("unexpected ACP response identity")
		}
		if m.Error != nil {
			return nil, fmt.Errorf("DSH ACP %s failed (code %d)", method, m.Error.Code)
		}
		if len(m.Result) == 0 {
			return nil, fmt.Errorf("ACP response omitted result")
		}
		return m.Result, nil
	}
}
