package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

type Message struct {
	Type      StreamMessageType
	Data      string
	Timestamp time.Time
	Success   *bool
	ExitCode  *int
}

type streamFrame struct {
	Type      StreamMessageType `json:"type"`
	Data      string            `json:"data"`
	Timestamp time.Time         `json:"timestamp"`
	Success   *bool             `json:"success,omitempty"`
	ExitCode  *int              `json:"exitCode,omitempty"`
}

type Broadcaster struct {
	mu         sync.Mutex
	messageLog []Message
	completed  bool
	notify     chan struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		messageLog: make([]Message, 0, 100),
		notify:     make(chan struct{}),
	}
}

func (b *Broadcaster) StreamTo(ctx context.Context, writer io.Writer) {
	cursor := 0
	for {
		b.mu.Lock()
		batch := append([]Message(nil), b.messageLog[cursor:]...)
		cursor = len(b.messageLog)
		done := b.completed
		wait := b.notify
		b.mu.Unlock()

		for _, msg := range batch {
			if !writeFrame(writer, msg) {
				return
			}
			if msg.Type == StreamTypeComplete {
				return
			}
		}

		if done {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-wait:
		}
	}
}

func (b *Broadcaster) appendLocked(msg Message) {
	b.messageLog = append(b.messageLog, msg)
	close(b.notify)
	b.notify = make(chan struct{})
}

func (b *Broadcaster) Broadcast(msgType StreamMessageType, data string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.completed {
		return
	}
	b.appendLocked(Message{Type: msgType, Data: data, Timestamp: time.Now()})
}

func (b *Broadcaster) BroadcastComplete(success bool, exitCode int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.completeLocked(success, exitCode)
}

func (b *Broadcaster) BroadcastError(errorMsg string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.completed {
		return
	}
	b.appendLocked(Message{Type: StreamTypeError, Data: errorMsg, Timestamp: time.Now()})
	b.completeLocked(false, 1)
}

func (b *Broadcaster) completeLocked(success bool, exitCode int) {
	if b.completed {
		return
	}
	b.completed = true
	b.appendLocked(Message{Type: StreamTypeComplete, Timestamp: time.Now(), Success: &success, ExitCode: &exitCode})
}

func writeFrame(writer io.Writer, msg Message) bool {
	payload, err := json.Marshal(streamFrame{
		Type:      msg.Type,
		Data:      msg.Data,
		Timestamp: msg.Timestamp,
		Success:   msg.Success,
		ExitCode:  msg.ExitCode,
	})
	if err != nil {
		return false
	}

	if _, err := fmt.Fprintf(writer, "data: %s\n\n", payload); err != nil {
		return false
	}

	if flusher, ok := writer.(interface{ Flush() }); ok {
		defer func() { _ = recover() }()
		flusher.Flush()
	}
	return true
}

type BroadcasterProgressWriter struct {
	broadcaster *Broadcaster
}

func NewBroadcasterProgressWriter(broadcaster *Broadcaster) *BroadcasterProgressWriter {
	return &BroadcasterProgressWriter{broadcaster: broadcaster}
}

func (w *BroadcasterProgressWriter) WriteStdout(message string) {
	w.broadcaster.Broadcast(StreamTypeStdout, message)
}

func (w *BroadcasterProgressWriter) WriteStderr(message string) {
	w.broadcaster.Broadcast(StreamTypeStderr, message)
}

func (w *BroadcasterProgressWriter) WriteProgress(message string) {
	w.broadcaster.Broadcast(StreamTypeProgress, message)
}

func (w *BroadcasterProgressWriter) WriteError(message string) {
	w.broadcaster.BroadcastError(message)
}

func (w *BroadcasterProgressWriter) WriteMessage(messageType, message string) {
	switch messageType {
	case "stdout":
		w.WriteStdout(message)
	case "stderr":
		w.WriteStderr(message)
	case "progress":
		w.WriteProgress(message)
	case "error":
		w.WriteError(message)
	default:
		w.WriteProgress(message)
	}
}
