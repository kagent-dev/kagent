package sse

import (
	"bufio"
	"bytes"
	"io"
)

type Event struct {
	Event string `json:"event"`
	Data  []byte `json:"data"`
}

func StreamSseResponse(r io.ReadCloser) <-chan *Event {
	scanner := bufio.NewScanner(r)
	ch := make(chan *Event, 10)
	go func() {
		defer close(ch)
		defer r.Close()
		currentEvent := &Event{}
		for scanner.Scan() {
			line := scanner.Bytes()
			if bytes.HasPrefix(line, []byte("event:")) {
				currentEvent.Event = string(bytes.TrimPrefix(line, []byte("event:")))
			}
			if bytes.HasPrefix(line, []byte("data:")) {
				currentEvent.Data = bytes.TrimPrefix(line, []byte("data:"))
				ch <- currentEvent
				currentEvent = &Event{}
			}
		}
	}()
	return ch
}
