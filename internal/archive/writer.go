package archive

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	StreamTypeStdout = "stdout"
	StreamTypeStderr = "stderr"
	StreamTypeError  = "error"
)

type StreamMessage struct {
	Type      string    `json:"type"`
	Data      string    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

type OperationsProgressWriter struct {
	writer io.Writer
}

func NewOperationsProgressWriter(writer io.Writer) *OperationsProgressWriter {
	return &OperationsProgressWriter{writer: writer}
}

func (w *OperationsProgressWriter) WriteMessage(msgType string, data string) {
	message := StreamMessage{
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now(),
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		return
	}

	output := fmt.Sprintf("data: %s\n\n", messageBytes)

	_, err = w.writer.Write([]byte(output))
	if err != nil {
		return
	}

	if flusher, ok := w.writer.(interface{ Flush() }); ok {
		defer func() {
			_ = recover()
		}()
		flusher.Flush()
	}
}

func (w *OperationsProgressWriter) WriteError(data string) {
	w.WriteMessage(StreamTypeError, data)
}

func (w *OperationsProgressWriter) WriteStdout(data string) {
	w.WriteMessage(StreamTypeStdout, data)
}
