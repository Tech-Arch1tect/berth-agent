package operations

import (
	"fmt"
	"io"
	"os"
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

type Subscriber struct {
	ID     string
	Writer io.Writer
}

type Broadcaster struct {
	operationID  string
	subscribers  map[string]*Subscriber
	messageLog   []Message
	mu           sync.RWMutex
	started      bool
	completed    bool
	completeOnce sync.Once
}

func NewBroadcaster(operationID string) *Broadcaster {
	return &Broadcaster{
		operationID: operationID,
		subscribers: make(map[string]*Subscriber),
		messageLog:  make([]Message, 0, 100),
	}
}

func (b *Broadcaster) Subscribe(subscriberID string, writer io.Writer) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	fmt.Fprintf(os.Stderr, "[BROADCASTER] Subscriber %s joining operation %s\n", subscriberID, b.operationID)

	b.subscribers[subscriberID] = &Subscriber{
		ID:     subscriberID,
		Writer: writer,
	}

	for _, msg := range b.messageLog {
		if msg.Type == StreamTypeComplete {
			b.writeCompleteMessage(writer, msg)
		} else {
			b.writeMessage(writer, msg)
		}
	}

	fmt.Fprintf(os.Stderr, "[BROADCASTER] Subscriber %s received %d historical messages for operation %s\n",
		subscriberID, len(b.messageLog), b.operationID)

	return nil
}

func (b *Broadcaster) Unsubscribe(subscriberID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.subscribers, subscriberID)
	fmt.Fprintf(os.Stderr, "[BROADCASTER] Subscriber %s left operation %s. Remaining: %d\n",
		subscriberID, b.operationID, len(b.subscribers))
}

func (b *Broadcaster) Broadcast(msgType StreamMessageType, data string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.completed {
		return
	}

	msg := Message{
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now(),
	}

	b.messageLog = append(b.messageLog, msg)

	for _, sub := range b.subscribers {
		b.writeMessage(sub.Writer, msg)
	}
}

func (b *Broadcaster) BroadcastComplete(success bool, exitCode int) {
	b.completeOnce.Do(func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		b.completed = true

		msg := Message{
			Type:      StreamTypeComplete,
			Data:      "",
			Timestamp: time.Now(),
			Success:   &success,
			ExitCode:  &exitCode,
		}

		b.messageLog = append(b.messageLog, msg)

		for _, sub := range b.subscribers {
			b.writeCompleteMessage(sub.Writer, msg)
		}

		fmt.Fprintf(os.Stderr, "[BROADCASTER] Operation %s completed - success: %v, exitCode: %d, sent to %d subscribers\n",
			b.operationID, success, exitCode, len(b.subscribers))
	})
}

func (b *Broadcaster) BroadcastError(errorMsg string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.completed {
		return
	}

	b.completed = true

	msg := Message{
		Type:      StreamTypeError,
		Data:      errorMsg,
		Timestamp: time.Now(),
	}

	b.messageLog = append(b.messageLog, msg)

	for _, sub := range b.subscribers {
		b.writeMessage(sub.Writer, msg)
	}

	fmt.Fprintf(os.Stderr, "[BROADCASTER] Operation %s error broadcast to %d subscribers: %s\n",
		b.operationID, len(b.subscribers), errorMsg)
}

func (b *Broadcaster) writeMessage(writer io.Writer, msg Message) {
	output := fmt.Sprintf("data: {\"type\":\"%s\",\"data\":\"%s\",\"timestamp\":\"%s\"}\n\n",
		msg.Type,
		escapeJSON(msg.Data),
		msg.Timestamp.Format(time.RFC3339Nano))

	_, err := writer.Write([]byte(output))
	if err != nil {
		return
	}

	if flusher, ok := writer.(interface{ Flush() }); ok {
		defer func() { _ = recover() }()
		flusher.Flush()
	}
}

func (b *Broadcaster) writeCompleteMessage(writer io.Writer, msg Message) {
	if msg.Success == nil || msg.ExitCode == nil {
		return
	}

	output := fmt.Sprintf("data: {\"type\":\"%s\",\"success\":%v,\"exitCode\":%d,\"timestamp\":\"%s\"}\n\n",
		msg.Type,
		*msg.Success,
		*msg.ExitCode,
		msg.Timestamp.Format(time.RFC3339Nano))

	_, err := writer.Write([]byte(output))
	if err != nil {
		return
	}

	if flusher, ok := writer.(interface{ Flush() }); ok {
		defer func() { _ = recover() }()
		flusher.Flush()
	}
}

func (b *Broadcaster) MarkStarted() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.started = true
	fmt.Fprintf(os.Stderr, "[BROADCASTER] Operation %s execution started\n", b.operationID)
}

func (b *Broadcaster) IsStarted() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.started
}

func (b *Broadcaster) IsCompleted() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.completed
}

func escapeJSON(s string) string {

	result := ""
	for _, ch := range s {
		if ch == '"' {
			result += "\\\""
		} else if ch == '\\' {
			result += "\\\\"
		} else {
			result += string(ch)
		}
	}
	return result
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
