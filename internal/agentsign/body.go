package agentsign

import (
	"bytes"
	"io"
)

type limitedReader struct {
	reader    io.Reader
	remaining int64
}

func (l *limitedReader) Read(destination []byte) (int, error) {
	if l.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(destination)) > l.remaining {
		destination = destination[:l.remaining]
	}
	read, err := l.reader.Read(destination)
	l.remaining -= int64(read)
	return read, err
}

type replayBody struct {
	*bytes.Reader
}

func (replayBody) Close() error { return nil }

func newReplayBody(body []byte) io.ReadCloser {
	return replayBody{bytes.NewReader(body)}
}
