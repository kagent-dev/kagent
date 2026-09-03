package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type rpcFrame struct {
	message rpcMessage
	err     error
}

// rpcClient owns newline-delimited JSON-RPC framing for one App Server
// process. App Server notifications share the same stream as call responses.
type rpcClient struct {
	writer  io.WriteCloser
	writeMu sync.Mutex
	frames  chan rpcFrame
}

func newRPCClient(writer io.WriteCloser, reader io.Reader, maxFrameBytes int) *rpcClient {
	client := &rpcClient{writer: writer, frames: make(chan rpcFrame, 16)}
	go client.read(reader, maxFrameBytes)
	return client
}

func (c *rpcClient) read(reader io.Reader, max int) {
	defer close(c.frames)
	buffered := bufio.NewReaderSize(reader, max+1)
	for {
		line, err := buffered.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			c.frames <- rpcFrame{err: fmt.Errorf("codex frame exceeds %d bytes", max)}
			return
		}
		if len(line) > max {
			c.frames <- rpcFrame{err: fmt.Errorf("codex frame exceeds %d bytes", max)}
			return
		}
		if len(bytes.TrimSpace(line)) != 0 {
			var message rpcMessage
			if decodeErr := json.Unmarshal(line, &message); decodeErr != nil {
				c.frames <- rpcFrame{err: fmt.Errorf("decode Codex JSON-RPC frame: %w", decodeErr)}
				return
			}
			// The pinned App Server has omitted jsonrpc on responses in some
			// releases, so accept an absent version but reject an incompatible one.
			if message.JSONRPC != "" && message.JSONRPC != "2.0" {
				c.frames <- rpcFrame{err: fmt.Errorf("unsupported Codex JSON-RPC version %q", message.JSONRPC)}
				return
			}
			c.frames <- rpcFrame{message: message}
		}
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			c.frames <- rpcFrame{err: fmt.Errorf("read Codex JSON-RPC frame: %w", err)}
			return
		}
	}
}

func (c *rpcClient) call(ctx context.Context, id int, method string, params any) (json.RawMessage, error) {
	if err := c.write(rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(fmt.Sprintf("%d", id)), Method: method}, params); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case frame, ok := <-c.frames:
			if !ok {
				return nil, fmt.Errorf("codex protocol stream closed while awaiting %s", method)
			}
			if frame.err != nil {
				return nil, frame.err
			}
			message := frame.message
			if message.Method != "" {
				if len(message.ID) != 0 {
					return nil, fmt.Errorf("unsupported Codex server request %q", message.Method)
				}
				// Initialization calls can receive additive notifications before
				// their response. Turn notifications are consumed after turn/start.
				continue
			}
			if string(message.ID) != fmt.Sprintf("%d", id) {
				return nil, fmt.Errorf("unexpected Codex response ID %s", message.ID)
			}
			if message.Error != nil {
				return nil, fmt.Errorf("codex %s failed: %s", method, bounded(message.Error.Message))
			}
			return message.Result, nil
		}
	}
}

func (c *rpcClient) notify(method string, params any) error {
	return c.write(rpcMessage{JSONRPC: "2.0", Method: method}, params)
}

func (c *rpcClient) write(message rpcMessage, params any) error {
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		message.Params = raw
	}
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode Codex JSON-RPC frame: %w", err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.writer.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write Codex JSON-RPC frame: %w", err)
	}
	return nil
}

func responseThreadID(raw json.RawMessage) (string, error) {
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode Codex thread response: %w", err)
	}
	if response.Thread.ID == "" {
		return "", fmt.Errorf("codex thread response omitted thread ID")
	}
	return response.Thread.ID, nil
}

func responseTurnID(raw json.RawMessage) (string, error) {
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("decode Codex turn response: %w", err)
	}
	if response.Turn.ID == "" {
		return "", fmt.Errorf("codex turn response omitted turn ID")
	}
	return response.Turn.ID, nil
}
