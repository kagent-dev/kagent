package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

// frame is one stdout line of `omp --mode rpc`: a response to a command, a
// session event, or a host request. Only the members the adapter reads are
// decoded; Raw keeps the line for event-specific decoding.
type frame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Error   json.RawMessage `json:"error"`
	Data    json.RawMessage `json:"data"`
	Method  string          `json:"method"`
	Raw     json.RawMessage `json:"-"`
}

type frameResult struct {
	frame frame
	err   error
}

// rpcClient speaks omp's protocol v1: one JSON object per line in each
// direction, at most maxFrameBytes per stdout line. omp correlates a response
// with its command through the command's string id.
type rpcClient struct {
	mu     sync.Mutex
	writer io.Writer
	frames chan frameResult
	nextID int
}

func newRPCClient(stdin io.Writer, stdout io.Reader, maxFrameBytes int) *rpcClient {
	client := &rpcClient{writer: stdin, frames: make(chan frameResult, 64)}
	go client.read(stdout, maxFrameBytes)
	return client
}

func (c *rpcClient) read(stdout io.Reader, maxFrameBytes int) {
	defer close(c.frames)
	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, err := readLine(reader, maxFrameBytes)
		if len(bytes.TrimSpace(line)) > 0 {
			var decoded frame
			if decodeErr := json.Unmarshal(line, &decoded); decodeErr != nil {
				c.frames <- frameResult{err: fmt.Errorf("decode omp frame: %w", decodeErr)}
				return
			}
			decoded.Raw = append(json.RawMessage(nil), line...)
			c.frames <- frameResult{frame: decoded}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.frames <- frameResult{err: err}
			}
			return
		}
	}
}

// readLine returns one newline-terminated line without its newline, refusing
// a line longer than limit instead of buffering it without bound.
func readLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > limit {
			return nil, fmt.Errorf("omp frame exceeds %d bytes", limit)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return bytes.TrimSuffix(line, []byte("\n")), err
	}
}

// command is every command the adapter sends: get_state, get_session_stats,
// abort and prompt, the only one with a member of its own.
type command struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// send writes one command with a fresh id and returns that id.
func (c *rpcClient) send(kind, message string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	id := "kagent-" + strconv.Itoa(c.nextID)
	return id, c.writeLocked(command{ID: id, Type: kind, Message: message})
}

// write sends one frame that expects no response, such as a UI reply.
func (c *rpcClient) write(message any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writeLocked(message)
}

func (c *rpcClient) writeLocked(message any) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode omp frame: %w", err)
	}
	if _, err := c.writer.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write omp frame: %w", err)
	}
	return nil
}

// call sends a command and waits for its response. Frames that arrive before
// it are handed to other, which must not block; call is used only while no
// prompt is running, when those frames are status updates.
func (c *rpcClient) call(ctx context.Context, command string, other func(frame) error) (json.RawMessage, error) {
	id, err := c.send(command, "")
	if err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("omp %s: %w", command, ctx.Err())
		case result, ok := <-c.frames:
			if !ok {
				return nil, fmt.Errorf("omp exited before answering %s", command)
			}
			if result.err != nil {
				return nil, result.err
			}
			if result.frame.Type != "response" || result.frame.ID != id {
				if err := other(result.frame); err != nil {
					return nil, err
				}
				continue
			}
			if !result.frame.Success {
				return nil, fmt.Errorf("omp %s failed: %s", command, responseError(result.frame.Error))
			}
			return result.frame.Data, nil
		}
	}
}

// responseError reads a failed response's error, a string in protocol v1.
func responseError(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil && text != "" {
		return text
	}
	return string(raw)
}
